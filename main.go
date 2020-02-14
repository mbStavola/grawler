package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackdanger/collectlinks"
	"github.com/jrokun/crawler/pkg/robots"
)

const userAgent string = "Grawler"

type headerTransport struct{}

func (transport *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("User-Agent", userAgent)
	return http.DefaultTransport.RoundTrip(req)
}

type website struct {
	hostname string
	path     string
}

func main() {
	rand.Seed(time.Now().UnixNano())

	firstURL := flag.String("start", "https://crawler-test.com/", "First website to crawl")
	agents := flag.Int("agents", 2, "How many crawlers to run")
	queueSize := flag.Int("queueSize", 100, "Size of the backing queues")
	flag.Parse()

	client := &http.Client{
		Transport: &headerTransport{},
		Timeout:   1 * time.Second,
	}

	visited := &sync.Map{}
	visitingRules := &sync.Map{}

	vettingQueue := make(chan []url.URL, *queueSize)
	crawlingQueue := make(chan url.URL, *queueSize)
	finished := make(chan website, *queueSize)

	parsedURL, err := url.Parse(*firstURL)
	if err != nil {
		fmt.Println(err)
		return
	}

	vettingQueue <- []url.URL{*parsedURL}

	// Kick off crawling procedures
	for i := 0; i < *agents; i++ {
		go vet(client, crawlingQueue, vettingQueue, visited, visitingRules)
		go crawl(client, crawlingQueue, vettingQueue, finished)
		go display(finished)
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Crawler is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	urlTotal := 0
	visited.Range(func(key, value interface{}) bool {
		urlTotal++
		return true
	})

	domainTotal := 0
	visitingRules.Range(func(domain, value interface{}) bool {
		crawlRules := value.(robots.CrawlRules)
		fmt.Printf("Domain: %s\n%s", domain, crawlRules.String())
		domainTotal++
		return true
	})

	fmt.Printf("Crawled %d urls for %d unique sites\n", urlTotal, domainTotal)
	fmt.Printf("Queue stats: Vetting (%d), Crawling (%d), Printing (%d)\n", len(vettingQueue), len(crawlingQueue), len(finished))
}

func crawl(client *http.Client, crawlingQueue <-chan url.URL, vettingQueue chan<- []url.URL, finished chan<- website) {
	for {
		toCrawl := <-crawlingQueue

		// ! Be kind, don't slam
		time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)

		response, err := client.Get(toCrawl.String())
		if err != nil {
			fmt.Println(err)
			continue
		}
		defer response.Body.Close()

		if response.StatusCode > 399 || response.StatusCode < 200 {
			fmt.Printf("Status code %d %s\n", response.StatusCode, toCrawl.String())
			continue
		}

		allLinks := collectlinks.All(response.Body)

		urlsToVet := make([]url.URL, len(allLinks))
		for _, link := range allLinks {
			parsedURL, err := url.Parse(link)
			if err != nil {
				fmt.Println(err)
				continue
			}

			// ! Relative links need to use the crawling Hostname
			if parsedURL.Hostname() == "" {
				parsedURL.Host = toCrawl.Hostname()
			}

			// Assume http for scheme-less urls
			if parsedURL.Scheme == "" {
				parsedURL.Scheme = "http"
			}

			urlsToVet = append(urlsToVet, *parsedURL)
		}

		go func() { vettingQueue <- urlsToVet }()
		finished <- website{hostname: toCrawl.Hostname(), path: toCrawl.Path}
	}
}

func vet(client *http.Client, crawlingQueue chan<- url.URL, vettingQueue <-chan []url.URL, visited *sync.Map, visitingRules *sync.Map) {
	for {
		for _, toVet := range <-vettingQueue {
			if _, ok := visited.Load(toVet.String()); ok {
				continue
			}

			visited.Store(toVet.String(), true)

			hostname := toVet.Hostname()
			path := toVet.Path

			if hostname == "" {
				continue
			}

			if _, ok := visitingRules.Load(hostname); !ok {
				crawlRules, err := robots.FetchCrawlRules(client, hostname)
				if err != nil {
					fmt.Println(err)
					continue
				}
				visitingRules.Store(hostname, crawlRules)
			}

			rulesInterface, ok := visitingRules.Load(hostname)
			if !ok {
				continue
			}
			rules := rulesInterface.(robots.CrawlRules)

			if _, ok := rules.AllowedPaths[path]; ok {
				// ? is there a better way to do this?
			} else if _, ok := rules.DisallowedPaths[path]; ok {
				continue
			}

			crawlingQueue <- toVet
		}
	}
}

func display(finished <-chan website) {
	for {
		website := <-finished
		fmt.Printf("Crawled: %v\n", website)
	}
}
