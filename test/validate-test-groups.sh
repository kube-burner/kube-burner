#!/bin/bash
# Script to validate test group mappings
# This helps ensure that our test filters match actual test names

set -euo pipefail

echo "🔍 Validating Test Group Mappings"
echo "=================================="

# Get all test names from the bats file
echo "📋 All available tests:"
grep -n "@test" test-k8s.bats | sed 's/.*@test "\(.*\)".*/\1/' | nl

echo ""
echo "🎯 Testing filter patterns:"

# Test each filter pattern
declare -A test_groups=(
    ["churn"]="churn=true"
    ["gc"]="gc=false"
    ["indexing"]="indexing=true|local-indexing=true"
    ["kubeconfig"]="kubeconfig"
    ["kubevirt"]="kubevirt|vm-latency"
    ["alert"]="check-alerts|alerting=true"
    ["crd"]="crd"
    ["delete"]="delete=true"
    ["read"]="read"
    ["sequential"]="sequential patch"
    ["userdata"]="user data file"
    ["datavolume"]="datavolume latency"
    ["metrics"]="metrics aggregation|metrics-endpoint=true"
)

for group in "${!test_groups[@]}"; do
    pattern="${test_groups[$group]}"
    echo "Group: $group"
    echo "Pattern: $pattern"
    
    # Use bats --list to see which tests would be selected
    if command -v bats >/dev/null 2>&1; then
        matches=$(bats --list test-k8s.bats | grep -E "$pattern" || true)
        if [[ -n "$matches" ]]; then
            echo "✅ Matches found:"
            echo "$matches" | sed 's/^/  /'
        else
            echo "❌ No matches found for pattern: $pattern"
        fi
    else
        # Fallback: grep the test file directly
        matches=$(grep "@test" test-k8s.bats | grep -E "$pattern" || true)
        if [[ -n "$matches" ]]; then
            echo "✅ Matches found:"
            echo "$matches" | sed 's/^/  /'
        else
            echo "❌ No matches found for pattern: $pattern"
        fi
    fi
    echo ""
done

echo "🏁 Validation complete!"
echo ""
echo "💡 Tips:"
echo "- If a group shows no matches, update the pattern in Makefile"
echo "- Test patterns should match the actual @test descriptions"
echo "- Use | for OR conditions in patterns"
