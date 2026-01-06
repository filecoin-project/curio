#!/bin/bash
# Coverage Report Generator for Curio
# This script merges multiple Go coverage profiles and generates reports

set -e

COVERAGE_DIR="${COVERAGE_DIR:-coverage}"
ARTIFACTS_DIR="${ARTIFACTS_DIR:-coverage-artifacts}"
OUTPUT_FILE="${OUTPUT_FILE:-${COVERAGE_DIR}/merged.out}"
SUMMARY_FILE="${SUMMARY_FILE:-${COVERAGE_DIR}/coverage.txt}"
HTML_FILE="${HTML_FILE:-${COVERAGE_DIR}/coverage.html}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "🔍 Curio Coverage Report Generator"
echo "=================================="

# Create output directory
mkdir -p "${COVERAGE_DIR}"

# Initialize merged coverage file with mode line
echo "mode: atomic" > "${OUTPUT_FILE}"

# Counter for processed files
PROCESSED=0

# Find and merge all coverage files
echo "📦 Searching for coverage files in ${ARTIFACTS_DIR}..."
if [ -d "${ARTIFACTS_DIR}" ]; then
    for dir in "${ARTIFACTS_DIR}"/coverage-*; do
        if [ -d "$dir" ]; then
            for file in "$dir"/*.out; do
                if [ -f "$file" ]; then
                    echo "  ✓ Processing $(basename "$file")"
                    # Skip the mode line and append the rest
                    tail -n +2 "$file" >> "${OUTPUT_FILE}"
                    PROCESSED=$((PROCESSED + 1))
                fi
            done
        fi
    done
else
    echo "⚠️  Warning: Artifacts directory not found: ${ARTIFACTS_DIR}"
    # Try to find coverage files in current directory
    for file in *.out; do
        if [ -f "$file" ] && [ "$file" != "merged.out" ]; then
            echo "  ✓ Processing $(basename "$file")"
            if [ $PROCESSED -eq 0 ]; then
                # First file - include mode line
                cat "$file" > "${OUTPUT_FILE}"
            else
                # Subsequent files - skip mode line
                tail -n +2 "$file" >> "${OUTPUT_FILE}"
            fi
            PROCESSED=$((PROCESSED + 1))
        fi
    done
fi

if [ $PROCESSED -eq 0 ]; then
    echo "❌ No coverage files found!"
    exit 1
fi

echo "✅ Merged $PROCESSED coverage file(s)"

# Generate function-level coverage report
echo ""
echo "📊 Generating coverage summary..."
go tool cover -func="${OUTPUT_FILE}" > "${SUMMARY_FILE}"

# Extract total coverage
TOTAL_COVERAGE=$(grep "total:" "${SUMMARY_FILE}" | awk '{print $3}')
COVERAGE_NUM=$(echo "${TOTAL_COVERAGE}" | sed 's/%//')

echo ""
echo "=================================="
if (( $(echo "$COVERAGE_NUM >= 80" | bc -l) )); then
    echo -e "${GREEN}✅ Total Coverage: ${TOTAL_COVERAGE}${NC}"
    BADGE_COLOR="brightgreen"
elif (( $(echo "$COVERAGE_NUM >= 60" | bc -l) )); then
    echo -e "${YELLOW}⚠️  Total Coverage: ${TOTAL_COVERAGE}${NC}"
    BADGE_COLOR="yellow"
else
    echo -e "${RED}❌ Total Coverage: ${TOTAL_COVERAGE}${NC}"
    BADGE_COLOR="red"
fi
echo "=================================="

# Generate HTML report
echo ""
echo "📄 Generating HTML report..."
go tool cover -html="${OUTPUT_FILE}" -o "${HTML_FILE}"
echo "  ✓ HTML report: ${HTML_FILE}"

# Display top 10 packages by coverage
echo ""
echo "📈 Top Packages by Coverage:"
echo "----------------------------"
head -n -1 "${SUMMARY_FILE}" | tail -n +2 | sort -k3 -rn | head -10 | \
    awk '{printf "  %s: %s\n", $3, $1}'

# Display bottom 10 packages (needing improvement)
echo ""
echo "⚠️  Packages Needing Coverage Improvement:"
echo "-------------------------------------------"
head -n -1 "${SUMMARY_FILE}" | tail -n +2 | sort -k3 -n | head -10 | \
    awk '{printf "  %s: %s\n", $3, $1}'

# Export coverage info for CI
if [ -n "$GITHUB_ENV" ]; then
    echo "COVERAGE_TOTAL=${TOTAL_COVERAGE}" >> "$GITHUB_ENV"
    echo "BADGE_COLOR=${BADGE_COLOR}" >> "$GITHUB_ENV"
    echo "COVERAGE_NUM=${COVERAGE_NUM}" >> "$GITHUB_ENV"
fi

# Generate JSON report for programmatic use
echo ""
echo "💾 Generating JSON report..."
cat > "${COVERAGE_DIR}/coverage.json" <<EOF
{
  "total_coverage": "${TOTAL_COVERAGE}",
  "coverage_percentage": ${COVERAGE_NUM},
  "badge_color": "${BADGE_COLOR}",
  "files_processed": ${PROCESSED},
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "report_files": {
    "merged": "${OUTPUT_FILE}",
    "summary": "${SUMMARY_FILE}",
    "html": "${HTML_FILE}"
  }
}
EOF
echo "  ✓ JSON report: ${COVERAGE_DIR}/coverage.json"

echo ""
echo "✅ Coverage report generation complete!"

