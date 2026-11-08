package also

// Policy signatures use plain Ed25519 keys kept outside the policy file. The
// policy names approvers and roles; the verifier trusts a key only when it is
// configured locally or through ALSO_TRUSTED_POLICY_KEYS. A policy cannot make
// itself trusted by naming its own key.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	PolicySigFile   = "also.policy.sig"
	SchemaPolicySig = "also.policy.sig/v1"

	EnvTrustedPolicyKeys = "ALSO_TRUSTED_POLICY_KEYS"
	TrustedKeysFile      = "trusted-policy-keys"
)

type PolicySignature struct {
	Key       string    `json:"key"` // base64 Ed25519 public key
	Signature string    `json:"signature"`
	SignedAt  time.Time `json:"signed_at,omitempty"`
}

type PolicySignatureFile struct {
	Schema     string            `json:"schema"`
	Signatures []PolicySignature `json:"signatures"`
}

type PolicyVerification struct {
	File         string   `json:"file"`
	Signed       bool     `json:"signed"`
	Valid        bool     `json:"valid"`
	Signatures   int      `json:"signatures"`
	Required     int      `json:"required"`
	Roles        []string `json:"roles,omitempty"`
	MissingRoles []string `json:"missing_roles,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// PolicySignaturePath maps also.policy.yml to also.policy.sig.
func PolicySignaturePath(policyPath string) string {
	ext := filepath.Ext(policyPath)
	return strings.TrimSuffix(policyPath, ext) + ".sig"
}

func LoadPolicySignatures(policyPath string) (*PolicySignatureFile, error) {
	path := PolicySignaturePath(policyPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PolicySignatureFile{Schema: SchemaPolicySig}, nil
		}
		return nil, fmt.Errorf("read policy signatures %s: %w", path, err)
	}
	var f PolicySignatureFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse policy signatures %s: %w", path, err)
	}
	if f.Schema == "" {
		f.Schema = SchemaPolicySig
	}
	if f.Schema != SchemaPolicySig {
		return nil, fmt.Errorf("unsupported policy signature schema %q in %s", f.Schema, path)
	}
	return &f, nil
}

func SavePolicySignatures(policyPath string, f *PolicySignatureFile) error {
	f.Schema = SchemaPolicySig
	sort.Slice(f.Signatures, func(i, j int) bool { return f.Signatures[i].Key < f.Signatures[j].Key })
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(PolicySignaturePath(policyPath), data, 0o644)
}

// GeneratePolicyKey writes a private key and returns the base64 public key.
func GeneratePolicyKey(privatePath, publicPath string) (string, error) {
	if privatePath == "" {
		return "", fmt.Errorf("private key path is required")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	encodedPub := base64.StdEncoding.EncodeToString(pub)
	if err := os.WriteFile(privatePath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write private key: %w", err)
	}
	if publicPath != "" {
		if err := os.WriteFile(publicPath, []byte(encodedPub+"\n"), 0o644); err != nil {
			return "", fmt.Errorf("write public key: %w", err)
		}
	}
	return encodedPub, nil
}

func ParsePolicyPublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 public key")
	}
	return ed25519.PublicKey(raw), nil
}

func LoadPolicyPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key")
	}
	return ed25519.PrivateKey(raw), nil
}

// SignPolicy adds or replaces one signature over the exact committed policy
// bytes. The signer must be listed in governance.approvers.
func SignPolicy(project, privatePath string) (*PolicySignature, error) {
	return SignPolicyFile(PolicyPath(project), privatePath)
}

func SignPolicyFile(policyPath, privatePath string) (*PolicySignature, error) {
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("read policy %s: %w", policyPath, err)
	}
	policy, err := LoadPolicyFile(policyPath)
	if err != nil {
		return nil, err
	}
	if policy == nil || policy.Governance == nil {
		return nil, fmt.Errorf("policy has no governance block; add approvers before signing")
	}
	priv, err := LoadPolicyPrivateKey(privatePath)
	if err != nil {
		return nil, err
	}
	pub := base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
	approver := policy.ApproverFor(pub)
	if approver == nil {
		return nil, fmt.Errorf("signing key is not listed in governance.approvers")
	}
	sig := PolicySignature{
		Key: pub, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)),
		SignedAt: time.Now().UTC(),
	}
	file, err := LoadPolicySignatures(policyPath)
	if err != nil {
		return nil, err
	}
	replaced := false
	for i := range file.Signatures {
		if file.Signatures[i].Key == pub {
			file.Signatures[i] = sig
			replaced = true
			break
		}
	}
	if !replaced {
		file.Signatures = append(file.Signatures, sig)
	}
	if err := SavePolicySignatures(policyPath, file); err != nil {
		return nil, err
	}
	return &sig, nil
}

func (p *Policy) ApproverFor(publicKey string) *PolicyApprover {
	if p == nil || p.Governance == nil {
		return nil
	}
	for i := range p.Governance.Approvers {
		if p.Governance.Approvers[i].Key == publicKey {
			return &p.Governance.Approvers[i]
		}
	}
	return nil
}

// VerifyPolicy verifies the project policy against the trusted key set.
func VerifyPolicy(project string) (*PolicyVerification, error) {
	files := PolicyFiles(project)
	if len(files) == 0 {
		return nil, nil
	}
	combined := &PolicyVerification{File: files[0], Valid: true}
	for _, file := range files {
		status, err := VerifyPolicyFile(file)
		if err != nil {
			return nil, err
		}
		if status == nil {
			continue
		}
		policy, perr := LoadPolicyFile(file)
		if perr != nil {
			return nil, perr
		}
		requires := policy != nil && policy.Governance != nil && policy.Governance.RequireSignature
		combined.Signed = combined.Signed || status.Signed
		combined.Signatures += status.Signatures
		if status.Required > combined.Required {
			combined.Required = status.Required
		}
		combined.Roles = UniqueStrings(append(combined.Roles, status.Roles...))
		combined.MissingRoles = UniqueStrings(append(combined.MissingRoles, status.MissingRoles...))
		if (requires && !status.Valid) || (status.Signed && !status.Valid) {
			combined.Valid = false
			if combined.Error == "" {
				combined.Error = filepath.Base(filepath.Dir(file)) + ": " + status.Error
			}
		}
	}
	return combined, nil
}

func VerifyPolicyFile(policyPath string) (*PolicyVerification, error) {
	data, err := os.ReadFile(policyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	policy, err := LoadPolicyFile(policyPath)
	if err != nil {
		return nil, err
	}
	status := &PolicyVerification{File: policyPath}
	if policy == nil || policy.Governance == nil {
		status.Error = "policy has no governance block"
		sigs, _ := LoadPolicySignatures(policyPath)
		if sigs != nil && len(sigs.Signatures) > 0 {
			status.Signed = true
		}
		return status, nil
	}
	gov := policy.Governance
	status.Required = gov.RequiredApprovals
	if status.Required <= 0 {
		status.Required = 1
	}
	file, err := LoadPolicySignatures(policyPath)
	if err != nil {
		return nil, err
	}
	if len(file.Signatures) == 0 {
		status.Error = "policy is not signed"
		return status, nil
	}
	status.Signed = true
	trusted, err := TrustedPolicyKeys()
	if err != nil {
		return nil, err
	}
	if len(trusted) == 0 {
		status.Error = "no trusted policy keys configured; set " + EnvTrustedPolicyKeys + " or run also policy trust"
		return status, nil
	}
	trustedSet := map[string]bool{}
	for _, key := range trusted {
		trustedSet[key] = true
	}
	seen := map[string]bool{}
	roles := map[string]bool{}
	for _, sig := range file.Signatures {
		if !trustedSet[sig.Key] {
			status.Error = "signature by an untrusted key"
			return status, nil
		}
		approver := policy.ApproverFor(sig.Key)
		if approver == nil {
			status.Error = "signature by a key that is not in governance.approvers"
			return status, nil
		}
		pub, err := ParsePolicyPublicKey(sig.Key)
		if err != nil {
			status.Error = err.Error()
			return status, nil
		}
		rawSig, err := base64.StdEncoding.DecodeString(sig.Signature)
		if err != nil || !ed25519.Verify(pub, data, rawSig) {
			status.Error = "invalid signature from " + approver.Name
			return status, nil
		}
		if !seen[sig.Key] {
			status.Signatures++
			seen[sig.Key] = true
		}
		if approver.Role != "" {
			roles[approver.Role] = true
		}
	}
	for role := range roles {
		status.Roles = append(status.Roles, role)
	}
	sort.Strings(status.Roles)
	for _, role := range gov.RequiredRoles {
		if !roles[role] {
			status.MissingRoles = append(status.MissingRoles, role)
		}
	}
	sort.Strings(status.MissingRoles)
	if len(status.MissingRoles) > 0 {
		status.Error = "missing required role(s): " + strings.Join(status.MissingRoles, ", ")
		return status, nil
	}
	if status.Signatures < status.Required {
		status.Error = fmt.Sprintf("need %d approval(s), have %d", status.Required, status.Signatures)
		return status, nil
	}
	status.Valid = true
	return status, nil
}

// LoadVerifiedPolicy is the install/upgrade path. A policy that declares
// require_signature must verify; a signed policy with a bad signature always
// fails, even if require_signature is false.
func LoadVerifiedPolicy(project string) (*Policy, error) {
	p, err := LoadPolicy(project)
	if err != nil || p == nil {
		return p, err
	}
	status, err := VerifyPolicy(project)
	if err != nil {
		return nil, err
	}
	if p.Governance != nil && p.Governance.RequireSignature && (status == nil || !status.Valid) {
		msg := "policy is not signed"
		if status != nil && status.Error != "" {
			msg = status.Error
		}
		return nil, fmt.Errorf("policy %s: %s", PolicyPath(project), msg)
	}
	if status != nil && status.Signed && !status.Valid {
		return nil, fmt.Errorf("policy %s: %s", status.File, status.Error)
	}
	return p, nil
}

// TrustedPolicyKeys reads keys from the environment and ~/.also/trusted-policy-keys.
func TrustedPolicyKeys() ([]string, error) {
	var out []string
	if value := os.Getenv(EnvTrustedPolicyKeys); value != "" {
		for _, token := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
		}) {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if _, err := ParsePolicyPublicKey(token); err != nil {
				return nil, fmt.Errorf("%s contains an invalid public key", EnvTrustedPolicyKeys)
			}
			out = append(out, token)
		}
	}
	data, err := os.ReadFile(filepath.Join(HomeDir(), TrustedKeysFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		key := fields[len(fields)-1]
		if _, err := ParsePolicyPublicKey(key); err != nil {
			return nil, fmt.Errorf("invalid public key in %s", filepath.Join(HomeDir(), TrustedKeysFile))
		}
		out = append(out, key)
	}
	return UniqueStrings(out), nil
}

func AddTrustedPolicyKey(publicKey, name string) error {
	if _, err := ParsePolicyPublicKey(publicKey); err != nil {
		return err
	}
	path := filepath.Join(HomeDir(), TrustedKeysFile)
	existing, _ := os.ReadFile(path)
	line := publicKey
	if name != "" {
		line = name + " " + publicKey
	}
	for _, l := range strings.Split(string(existing), "\n") {
		fields := strings.Fields(l)
		if len(fields) > 0 && fields[len(fields)-1] == publicKey {
			return nil
		}
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}
	existing = append(existing, []byte(line+"\n")...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, existing, 0o600)
}

func RemoveTrustedPolicyKey(value string) (bool, error) {
	path := filepath.Join(HomeDir(), TrustedKeysFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	removed := false
	var keep []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if fields[len(fields)-1] == value || (len(fields) > 1 && fields[0] == value) {
			removed = true
			continue
		}
		keep = append(keep, line)
	}
	if !removed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
}
