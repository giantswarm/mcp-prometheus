package prometheus

import (
	"strings"
	"testing"
)

// The facts below fix agent mistakes recorded in the PRs they cite. Reword a
// description freely, but keep the fact. Before cutting any other sentence,
// find its pull request with git log -S'<sentence>' and read why it is there.

// TestGetRulesPrefersFilters: an unfiltered get_rules dump is far above the
// truncation cap on a management cluster (#288).
func TestGetRulesPrefersFilters(t *testing.T) {
	d := registeredTool(t, toolGetRules).Description
	if !strings.Contains(d, "exclude_alerts") {
		t.Errorf("get_rules lost a fact from #288: prefer the filters and exclude_alerts over a full dump, which exceeds the truncation cap")
	}
}

// TestRulerToolsSayWhenOrgIDIsNeeded: a ruler call without a tenant fails on
// Mimir with "no valid org id found" when no default tenant is set (#288);
// the ruler reads one tenant per call (#340).
func TestRulerToolsSayWhenOrgIDIsNeeded(t *testing.T) {
	for _, name := range []string{toolGetAlerts, toolGetRules} {
		d := registeredTool(t, name).Description
		for _, fact := range []string{"pass org_id", "default tenant"} {
			if !strings.Contains(d, fact) {
				t.Errorf("%s lost a fact from #288/#340 (%q): the Mimir ruler reads one tenant per call and a bare call without a default tenant fails", name, fact)
			}
		}
	}
}
