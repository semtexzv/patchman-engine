# This identity contains account_name = "0"

set -x 
ACCOUNT_NUMER=${1:-"1"}
JSON='{"entitlements":{"smart_management":{"is_entitled":true}},"identity":{"account_number":"'$ACCOUNT_NUMER'","type":"User"}}'
export IDENTITY=$(echo "$JSON" | base64 -w 0 -)
echo $IDENTITY
