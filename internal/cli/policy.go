package cli

import (
	"fmt"
	"os"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func newPolicyKeygenCmd() *cobra.Command {
	var (
		private string
		public  string
		force   bool
		jsonOut bool
	)
	c := newCmd("keygen", "Generate an Ed25519 key for signing also.policy.yml")
	c.Long = `Writes a private key in base64 at --private (mode 0600) and prints the
base64 public key. Put the public key in governance.approvers and trust it
locally with "also policy trust" or ALSO_TRUSTED_POLICY_KEYS.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 || private == "" {
			return fmt.Errorf("usage: also policy keygen --private <path> [--public <path>]")
		}
		if !force {
			if _, err := os.Stat(private); err == nil {
				return fmt.Errorf("%s already exists; use --force to replace it", private)
			}
		}
		pub, err := also.GeneratePolicyKey(private, public)
		if err != nil {
			return err
		}
		if jsonOut {
			return printJSON(map[string]any{"public_key": pub, "private_key": private, "public_file": public})
		}
		fmt.Println(pub)
		fmt.Fprintf(os.Stderr, "private key: %s\n", private)
		if public != "" {
			fmt.Fprintf(os.Stderr, "public key:  %s\n", public)
		}
		return nil
	}
	c.Flags().StringVar(&private, "private", "", "private key output path")
	c.Flags().StringVar(&public, "public", "", "public key output path")
	c.Flags().BoolVar(&force, "force", false, "replace an existing private key")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func newPolicyTrustCmd() *cobra.Command {
	var (
		key     string
		name    string
		remove  string
		list    bool
		jsonOut bool
	)
	c := newCmd("trust", "Manage the trusted policy signing keys")
	c.Long = `Trust is separate from the policy file: a key is trusted only when it is in
~/.also/trusted-policy-keys or ALSO_TRUSTED_POLICY_KEYS. A policy cannot make
itself trusted by naming its own key.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also policy trust [--key <pub>] [--list] [--remove <key|name>]")
		}
		if list {
			keys, err := also.TrustedPolicyKeys()
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(map[string]any{"keys": keys})
			}
			for _, k := range keys {
				fmt.Println(k)
			}
			return nil
		}
		if remove != "" {
			removed, err := also.RemoveTrustedPolicyKey(remove)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(map[string]any{"removed": removed})
			}
			if !removed {
				return fmt.Errorf("key or name %q is not trusted", remove)
			}
			return nil
		}
		if key == "" {
			return fmt.Errorf("usage: also policy trust --key <public-key> [--name <name>]")
		}
		if err := also.AddTrustedPolicyKey(key, name); err != nil {
			return err
		}
		if jsonOut {
			return printJSON(map[string]any{"trusted": key, "name": name})
		}
		fmt.Printf("trusted %s\n", short(key))
		return nil
	}
	c.Flags().StringVar(&key, "key", "", "base64 Ed25519 public key")
	c.Flags().StringVar(&name, "name", "", "optional local name")
	c.Flags().StringVar(&remove, "remove", "", "remove a key or name")
	c.Flags().BoolVar(&list, "list", false, "list trusted keys")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func newPolicySignCmd() *cobra.Command {
	var (
		key     string
		dir     string
		jsonOut bool
	)
	c := newCmd("sign", "Sign also.policy.yml with an approver key")
	c.Long = `Signs the exact bytes of also.policy.yml. The public half of --key must be
listed in governance.approvers. Sign again to replace your signature.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 || key == "" {
			return fmt.Errorf("usage: also policy sign --key <private-key> [--dir <path>]")
		}
		project, err := auditProject(dir)
		if err != nil {
			return err
		}
		sig, err := also.SignPolicy(project, key)
		if err != nil {
			return err
		}
		policy, err := also.LoadPolicy(project)
		if err != nil {
			return err
		}
		approver := policy.ApproverFor(sig.Key)
		if jsonOut {
			return printJSON(map[string]any{"key": sig.Key, "signature": sig.Signature, "approver": approver})
		}
		if approver != nil {
			fmt.Printf("signed by %s (%s)\n", approver.Name, orDash(approver.Role))
		} else {
			fmt.Println("signed")
		}
		return nil
	}
	c.Flags().StringVar(&key, "key", "", "private key path")
	c.Flags().StringVarP(&dir, "dir", "p", "", "directory whose policy to sign (default: current directory)")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	return c
}

func newPolicyVerifyCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
		ci      bool
	)
	c := newCmd("verify", "Verify policy signatures, roles and approvals")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also policy verify [--ci] [--json]")
		}
		project, err := auditProject(dir)
		if err != nil {
			return err
		}
		status, err := also.VerifyPolicy(project)
		if err != nil {
			return err
		}
		if jsonOut {
			if err := printJSON(status); err != nil {
				return err
			}
		} else if status == nil {
			fmt.Println("no policy")
			return nil
		} else {
			printPolicySignatureMustLoad(project, status)
		}
		if ci && status != nil && !status.Valid {
			return fmt.Errorf("policy verification failed")
		}
		return nil
	}
	c.Flags().StringVarP(&dir, "dir", "p", "", "directory whose policy to verify (default: current directory)")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().BoolVar(&ci, "ci", false, "exit 1 unless the policy verifies")
	return c
}

func printPolicySignatureMustLoad(project string, status *also.PolicyVerification) {
	p, err := also.LoadPolicy(project)
	if err != nil {
		fmt.Println(status.Error)
		return
	}
	printPolicySignature(p, status)
}
