package robots

import (
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Set provides a convience typedef for what is essentially a hashset
type Set = map[string]bool

// RulesIndex represents a collection of access-rules by domain
// These will be parsed from the domain's robots.txt
type RulesIndex struct {
	// Internal http Client
	client *http.Client

	// A mapping of domain to robots.txt rules
	rules map[string]CrawlRules
}

// NewRulesIndex will construct a new RulesIndex instance
// If no http.Client is provided, we'll use the default one
func NewRulesIndex(client *http.Client) RulesIndex {
	if client == nil {
		client = http.DefaultClient
	}

	return RulesIndex{
		client,
		make(map[string]CrawlRules),
	}
}

// Get will do one of two things:
// 1) Return the cached CrawlRules for a given domain
// 2) Fetch, parse, and store the given domain's robots.txt as CrawlRules
//
// This method call has the potential (obviously) to result in a network call
//
// Be aware that there is no expiration on the cached rules for the lifetime of the index.
func (index *RulesIndex) Get(hostname string) (CrawlRules, error) {
	if _, ok := index.rules[hostname]; !ok {
		crawlRules, err := fetchCrawlRules(index.client, hostname)
		if err != nil {
			return CrawlRules{}, err
		}
		index.rules[hostname] = crawlRules
	}

	rules := index.rules[hostname]
	return rules, nil
}

// DomainCount simply provides a count of all the domains indexed
func (index *RulesIndex) DomainCount() int {
	return len(index.rules)
}

func (index *RulesIndex) String() string {
	ret := ""
	for domain, rules := range index.rules {
		ret += fmt.Sprintf("Domain: %s\n%s", domain, rules.String())
	}
	return ret
}

// CrawlRules is the representation of a site's robots.txt
type CrawlRules struct {
	// None of these paths can be accessed
	DisallowedPaths Set

	// These paths override any rule in DisallowedPaths
	AllowedPaths Set

	// How long a crawler should wait before hitting a domain again
	Delay time.Duration
}

// Test Given a path, test if the rules for this domain grant access
func (rules *CrawlRules) Test(path string) bool {
	if _, ok := rules.AllowedPaths[path]; ok {
		return true
	}

	_, ok := rules.DisallowedPaths[path]
	return !ok
}

func (rules *CrawlRules) String() string {
	allowedPaths := ""
	for path := range rules.AllowedPaths {
		allowedPaths += fmt.Sprintf("\t%s\n", path)
	}

	disallowedPaths := ""
	for path := range rules.DisallowedPaths {
		disallowedPaths += fmt.Sprintf("\t%s\n", path)
	}

	return fmt.Sprintf("Delay: %v\nAllowed:\n%sDisallowed:\n%s", rules.Delay, allowedPaths, disallowedPaths)
}

func newCrawlRules() CrawlRules {
	return CrawlRules{
		DisallowedPaths: make(Set),
		AllowedPaths:    make(Set),
		Delay:           1 * time.Second,
	}
}

func fetchCrawlRules(client *http.Client, domain string) (CrawlRules, error) {
	url := fmt.Sprintf("http://%s/robots.txt", domain)
	response, err := client.Get(url)
	if err != nil {
		return newCrawlRules(), err
	}
	defer response.Body.Close()

	if response.StatusCode > 399 || response.StatusCode < 200 {
		return newCrawlRules(), nil
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return newCrawlRules(), err
	}

	crawlRules := newCrawlRules()

	respectRules := false
	for _, line := range strings.Split(string(body), "\n") {
		// Ignore Comments
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		components := strings.SplitN(line, ": ", 2)
		if len(components) < 2 {
			continue
		}
		directive, value := strings.ToLower(components[0]), components[1]

		// We only care about the robots.txt rules if they're talking about us
		if directive == "user-agent" && (value == "*" || strings.ToLower(value) == "grawler") {
			respectRules = true
			continue
		} else if directive == "user-agent" {
			respectRules = false
		}

		if !respectRules {
			continue
		}

		switch directive {
		case "allow":
			path := strings.TrimSpace(value)
			crawlRules.AllowedPaths[path] = true
		case "disallow":
			path := strings.TrimSpace(value)
			crawlRules.DisallowedPaths[path] = true
		case "crawl-delay":
			count, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			crawlRules.Delay = time.Duration(int64(math.Min(30.0, float64(count)))) * time.Second
		}
	}

	return crawlRules, nil
}
