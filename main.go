package main

import (
	"flag"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/awalterschulze/gographviz"
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
	referrer url.URL

	url.URL
}

func main() {
	rand.Seed(time.Now().UnixNano())

	firstURL := flag.String("start", "https://crawler-test.com/", "First website to crawl")
	queueSize := flag.Int("queueSize", 100, "Size of the backing queues")
	flag.Parse()

	client := &http.Client{
		Transport: &headerTransport{},
		Timeout:   5 * time.Second,
	}

	parsedURL, err := url.Parse(*firstURL)
	if err != nil {
		fmt.Println(err)
		return
	}

	visited, visitingRules, finished := manager(client, *parsedURL, *queueSize)
	graph, err := printer(finished)

	if err != nil {
		fmt.Println(err)
		return
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Crawler is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	writeGraph(graph)

	urlTotal := 0
	for range visited {
		urlTotal++
	}

	domainTotal := 0
	for domain, crawlRules := range visitingRules {
		fmt.Printf("Domain: %s\n%s", domain, crawlRules.String())
		domainTotal++
	}

	fmt.Printf("Crawled %d urls for %d unique sites\n", urlTotal, domainTotal)
}

func manager(client *http.Client, initialURL url.URL, queueSize int) (visited robots.Set, visitingRules map[string]robots.CrawlRules, finished chan website) {
	visited = make(robots.Set)
	visitingRules = make(map[string]robots.CrawlRules)

	finished = make(chan website, queueSize)

	go func() {
		vettingQueue := make(chan []website, queueSize)
		vettingQueue <- []website{website{URL: initialURL}}

		for {
			for _, toVet := range <-vettingQueue {
				if _, ok := visited[toVet.String()]; ok {
					continue
				}

				visited[toVet.String()] = true

				hostname := toVet.Hostname()
				path := toVet.Path

				if hostname == "" {
					continue
				}

				if _, ok := visitingRules[hostname]; !ok {
					crawlRules, err := robots.FetchCrawlRules(client, hostname)
					if err != nil {
						fmt.Println(err)
						continue
					}
					visitingRules[hostname] = crawlRules
				}

				rules, ok := visitingRules[hostname]
				if !ok {
					continue
				}

				if _, ok := rules.AllowedPaths[path]; ok {
					// ? is there a better way to do this?
				} else if _, ok := rules.DisallowedPaths[path]; ok {
					continue
				}

				go crawl(client, toVet, vettingQueue, finished)
			}
		}
	}()

	return
}

func crawl(client *http.Client, toCrawl website, vettingQueue chan<- []website, finished chan<- website) {
	// ! Be kind, don't slam
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)

	response, err := client.Get(toCrawl.String())
	if err != nil {
		fmt.Println(err)
		return
	}
	defer response.Body.Close()

	if response.StatusCode > 399 || response.StatusCode < 200 {
		fmt.Printf("Status code %d %s\n", response.StatusCode, toCrawl.String())
		return
	}

	allLinks := collectlinks.All(response.Body)

	urlsToVet := make([]website, len(allLinks))
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

		toVet := website{referrer: toCrawl.URL, URL: *parsedURL}
		urlsToVet = append(urlsToVet, toVet)
	}

	vettingQueue <- urlsToVet
	finished <- toCrawl
}

func printer(finished <-chan website) (*gographviz.Graph, error) {
	graphAst, err := gographviz.ParseString(`digraph "Grawled Websites" {}`)
	if err != nil {
		return nil, err
	}

	graph := gographviz.NewGraph()
	if err := gographviz.Analyse(graphAst, graph); err != nil {
		return nil, err
	}

	graphName := `"Grawled Websites"`

	graph.SetName(graphName)
	graph.AddNode(graphName, "start", map[string]string{"label": "Start"})

	go func() {
		for {
			website := <-finished

			websiteHostname := fmt.Sprintf("\"%s\"", website.Hostname())
			websitePath := fmt.Sprintf("\"%s\"", website.EscapedPath())
			websiteGraphName := fmt.Sprintf("cluster_%s", hash(website.Hostname()))

			if !graph.IsSubGraph(websiteGraphName) {
				graph.AddSubGraph(graphName, websiteGraphName, graphAttributes(websiteHostname))
			}

			// Add the crawled site
			websiteNodeName := hashURL(website.URL)
			graph.AddNode(websiteGraphName, websiteNodeName, nodeAttributes(websitePath))

			// If there is no referrer, this must be the entrypoint into the system
			if website.referrer.Hostname() == "" {
				graph.AddEdge("start", websiteNodeName, true, map[string]string{})
			} else {
				reffererNodeName := hashURL(website.referrer)
				graph.AddEdge(reffererNodeName, websiteNodeName, true, map[string]string{})
			}

			fmt.Printf("Crawled: %s%s\n", website.Hostname(), website.Path)
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			writeGraph(graph)
		}
	}()

	return graph, nil
}

func writeGraph(graph *gographviz.Graph) {
	output := graph.String()
	if err := ioutil.WriteFile("grawled.gv", []byte(output), 0777); err != nil {
		fmt.Println(err)
	}
}

func graphAttributes(hostname string) map[string]string {
	return map[string]string{
		"label":   hostname,
		"nodesep": "6",
		"ranksep": "4",
		"style":   "dotted",
	}
}

func nodeAttributes(path string) map[string]string {
	return map[string]string{
		"label": path,
	}
}

func hashURL(url url.URL) string {
	token := fmt.Sprintf("%s%s", url.Hostname(), url.Path)
	return hash(token)
}

func hash(token string) string {
	hash := fnv.New32a()
	hash.Write([]byte(token))

	return fmt.Sprintf("h%0x", hash.Sum32())
}
