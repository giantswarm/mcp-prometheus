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
// Mimir with "no valid org id found" (#288), and under OAuth with several
// alerting tenants the tool refuses until org_id picks one (#340).
func TestRulerToolsSayWhenOrgIDIsNeeded(t *testing.T) {
	for _, name := range []string{toolGetAlerts, toolGetRules} {
		d := registeredTool(t, name).Description
		if !strings.Contains(d, "pass org_id") {
			t.Errorf("%s lost a fact from #288/#340: the Mimir ruler reads one tenant per call and needs org_id", name)
		}
	}
}
