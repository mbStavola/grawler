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

type CrawlRules struct {
	DisallowedPaths set
	AllowedPaths    set
	Delay           time.Duration
}

func newCrawlRules() CrawlRules {
	return CrawlRules{
		DisallowedPaths: make(set),
		AllowedPaths:    make(set),
		Delay:           1 * time.Second,
	}
}

func (crawlRules *CrawlRules) String() string {
	allowedPaths := ""
	for path := range crawlRules.AllowedPaths {
		allowedPaths += fmt.Sprintf("\t%s\n", path)
	}

	disallowedPaths := ""
	for path := range crawlRules.DisallowedPaths {
		disallowedPaths += fmt.Sprintf("\t%s\n", path)
	}

	return fmt.Sprintf("Delay: %v\nAllowed:\n%sDisallowed:\n%s", crawlRules.Delay, allowedPaths, disallowedPaths)
}

type set = map[string]bool

func FetchCrawlRules(client *http.Client, domain string) (CrawlRules, error) {
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
