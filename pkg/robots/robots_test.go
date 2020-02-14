package robots

import "testing"

func TestCrawlRulesTest(t *testing.T) {
	rules := newCrawlRules()

	rules.DisallowedPaths = NewSet([]string{
		"/bad",
		"/really-bad",
		"/bad-i-guess",
	})

	if rules.Test("/bad") {
		t.Errorf("Shouldn't be able to access /bad")
	} else if rules.Test("/really-bad") {
		t.Errorf("Shouldn't be able to access /really-bad")
	} else if rules.Test("/bad-i-guess") {
		t.Errorf("Shouldn't be able to access /bad-i-guess")
	}

	if !rules.Test("/this-should-work") {
		t.Errorf("Should be able to access /this-should-work")
	}
}
