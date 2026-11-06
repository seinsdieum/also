import json
import sys

data = json.load(open(sys.argv[1]))
servers = data.get("mcp", {}).get("servers", data.get("mcp", {}))
if "smoke" not in servers:
    raise SystemExit("smoke MCP server missing from " + sys.argv[1])
