#!/bin/sh
# Prints the expiry and scopes of the key in .env. Never prints the key itself.
set -eu
cd "$(dirname "$0")/.."
[ -f .env ] || { echo "No .env, see .env.example" >&2; exit 1; }
set -a; . ./.env; set +a
: "${KTALK_URL:?KTALK_URL is not set in .env}"
: "${KTALK_TOKEN:?KTALK_TOKEN is not set in .env}"
base="${KTALK_URL%/}/api"
echo "Space: ${KTALK_URL%/}"
curl -sS -m 20 -H "X-Auth-Token: $KTALK_TOKEN" "$base/domain/applications/access-info" | python3 -c '
import json, sys
info = json.load(sys.stdin)
print("Key expires:", info.get("expiredAt"))
scopes = {s["type"]: s["restrictionType"] for s in info.get("scopes", [])}
required = {"rooms": "readWrite", "profiles": "read", "calendar": "readWrite", "recording": "read",
            "activeRecordings": "readWrite", "reporting": "read", "onlineStats": "read", "webhooks": "readWrite"}
for scope, level in required.items():
    have = scopes.get(scope)
    ok = have == "readWrite" or (have and level == "read")
    print(("  ok      " if ok else "  MISSING ") + scope + " (" + level + ")")
'
printf 'Tariff expires: '
curl -sS -m 20 -H "X-Auth-Token: $KTALK_TOKEN" "$base/domain/stats/tariffExpirationDate"; echo
