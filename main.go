package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/yhat/scrape"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type Conference struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Year int    `json:"year"`
}

func (c *Conference) String() string {
	return fmt.Sprintf("%s %d", c.Name, c.Year)
}

type Config struct {
	fetchTimeout    time.Duration
	conferencesFile string
	outputDirectory string
	conferences     []Conference
}

var (
	config Config
)

type FetchError struct {
	Msg string
}

func (e FetchError) Error() string {
	return e.Msg
}

var (
	MissingDownloadLinkErr = FetchError{Msg:"no pdf download links found on page"}
	TooManyDownloadLinksErr = FetchError{Msg:"too many pdf download links found on page"}
)

func createConfDirectory(outputDirectory string, conf Conference) (string, error) {
	// create conference directory
	confDirectory := path.Join(outputDirectory, conf.Name, strconv.Itoa(conf.Year))
	if _, err := os.Stat(confDirectory); os.IsNotExist(err) {
		if err := os.MkdirAll(confDirectory, os.ModePerm); err != nil {
			return "", err
		}
	}
	return confDirectory, nil
}


func getFullUrl(baseUrl, linkUrl string) (string, error) {
	var fullUrl string

	link, err := url.Parse(linkUrl)
	if err != nil {
		return "", err
	}

	if link.Host == "" || link.Scheme == "" {
		base, err := url.Parse(baseUrl)
		if err != nil {
			return "", err
		}
		full, err := base.Parse(linkUrl)
		if err != nil {
			return "", err
		}
		fullUrl = full.String()
	} else {
		fullUrl = linkUrl
	}

	return fullUrl, nil
}

func downloadFile(url, filepath string) error {
	if _, err := os.Stat(filepath); !os.IsNotExist(err) {
		log.Printf("skipping download, file already exists: %s, \n", filepath)
		return nil
	}

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func getDownloadUrl(pageUrl string, matcher scrape.Matcher) (string, error) {
	response, err := http.Get(pageUrl)
	if err != nil {
		return "", err
	}

	root, err := html.Parse(response.Body)
	if err != nil {
		return "", err
	}

	// grab all paper links
	pageNodes := scrape.FindAll(root, matcher)
	if len(pageNodes) < 1 {
		return "", MissingDownloadLinkErr
	}

	fileUrl, err := getFullUrl(pageUrl, scrape.Attr(pageNodes[0], "href"))
	if err != nil {
		return "", err
	}

	if len(pageNodes) > 1 {
		return fileUrl, TooManyDownloadLinksErr
	}
	return fileUrl, nil
}

func getLinks(pageUrl string, matcher scrape.Matcher) ([]string, error) {
	response, err := http.Get(pageUrl)
	if err != nil {
		return nil, err
	}

	root, err := html.Parse(response.Body)
	if err != nil {
		return nil, err
	}

	// grab all paper links
	pageNodes := scrape.FindAll(root, matcher)
	pages := make([]string, 0)
	for _, page := range pageNodes {
		url, err := getFullUrl(pageUrl, scrape.Attr(page, "href"))
		if err != nil {
			log.Fatal(err)
		}
		pages = append(pages, url)
	}

	return pages, nil
}

// Pre-main bind flags to variables
func init() {
	flag.DurationVar(&config.fetchTimeout, "timeout", 500*time.Millisecond, "timeout between downloading papers")
	flag.StringVar(&config.conferencesFile, "config", "conferences.json", "JSON file listing conferences")
	flag.StringVar(&config.outputDirectory, "output-dir", "papers", "output directory for storing papers")
	flag.Parse()

	// create output directory
	if _, err := os.Stat(config.outputDirectory); os.IsNotExist(err) {
		if err := os.MkdirAll(config.outputDirectory, os.ModePerm); err != nil {
			log.Fatal(err)
		}
	}
}

func main() {
	conferencesFile, err := os.Open(config.conferencesFile)
	if err != nil {
		log.Fatal(err)
	}
	defer conferencesFile.Close()

	bytes, _ := ioutil.ReadAll(conferencesFile)
	json.Unmarshal(bytes, &config.conferences)

	for _, conf := range config.conferences {
		switch conf.Name {
		case "USENIX":
			confDirectory, err := createConfDirectory(config.outputDirectory, conf)
			if err != nil  {
				log.Fatal(err)
			}

			// define a matcher
			matcher := func(n *html.Node) bool {
				// must check for nil values
				if n.DataAtom == atom.A && n.Parent != nil && n.Parent.Parent != nil {
					return strings.Contains(scrape.Attr(n.Parent.Parent, "class"), "node-paper")
				}
				return false
			}
			pages, err := getLinks(conf.URL, matcher)
			if err != nil {
				log.Fatal(err)
			}

			for _, p := range pages {
				// define a matcher
				urlMatcher := func(n *html.Node) bool {
					// must check for nil values
					if n.DataAtom == atom.A && n.Parent != nil {
						return scrape.Attr(n.Parent, "class") == "file"
					}
					return false
				}
				downloadUrl, err := getDownloadUrl(p, urlMatcher)
				if err != nil {
					if err == MissingDownloadLinkErr {
						continue
					} else if err == TooManyDownloadLinksErr {
						log.Println(err)
					} else {
						log.Fatal(err)
					}
				}
				log.Println(downloadUrl)
				splitUrl := strings.Split(downloadUrl, "/")
				filepath := path.Join(confDirectory, splitUrl[len(splitUrl)-1])
				downloadFile(downloadUrl, filepath)
				time.Sleep(config.fetchTimeout)
			}
		case "NDSS":
			confDirectory, err := createConfDirectory(config.outputDirectory, conf)
			if err != nil  {
				log.Fatal(err)
			}

			switch {
			case conf.Year == 2018:
				matcher := func(n *html.Node) bool {
					// must check for nil values
					if n.DataAtom == atom.A {
						return scrape.Text(n) == "Paper"
					}
					return false
				}

				downloadLinks, err := getLinks(conf.URL, matcher)
				if err != nil {
					log.Fatal(err)
				}

				for _, link := range downloadLinks {
					log.Println(link)
					splitUrl := strings.Split(link, "/")
					filepath := path.Join(confDirectory, splitUrl[len(splitUrl)-1])
					downloadFile(link, filepath)
					time.Sleep(config.fetchTimeout)
				}
			case conf.Year == 2017 || conf.Year == 2015 || conf.Year == 2014:
				matcher := func(n *html.Node) bool {
					// must check for nil values
					if n.DataAtom == atom.A && n.Parent != nil {
						return n.Parent.DataAtom == atom.H3
					}
					return false
				}

				pages, err := getLinks(conf.URL, matcher)
				if err != nil {
					log.Fatal(err)
				}

				for _, p := range pages {
					urlMatcher := func(n *html.Node) bool {
						// must check for nil values
						if n.DataAtom == atom.A {
							return scrape.Text(n) == "Paper"
						}
						return false
					}

					downloadUrl, err := getDownloadUrl(p, urlMatcher)
					if err != nil {
						if err == MissingDownloadLinkErr {
							continue
						} else if err == TooManyDownloadLinksErr {
							log.Println(err)
						} else {
							log.Fatal(err)
						}
					}
					log.Println(downloadUrl)
					splitUrl := strings.Split(downloadUrl, "/")
					filepath := path.Join(confDirectory, splitUrl[len(splitUrl)-1])
					downloadFile(downloadUrl, filepath)
					time.Sleep(config.fetchTimeout)
				}
			case conf.Year == 2016:
				// define a matcher
				matcher := func(n *html.Node) bool {
					// must check for nil values
					if n.DataAtom == atom.A && n.Parent != nil {
						return n.Parent.DataAtom == atom.H3
					}
					return false
				}

				downloadLinks, err := getLinks(conf.URL, matcher)
				if err != nil {
					log.Fatal(err)
				}

				for _, link := range downloadLinks {
					log.Println(link)
					splitUrl := strings.Split(link, "/")
					filepath := path.Join(confDirectory, splitUrl[len(splitUrl)-1])
					downloadFile(link, filepath)
					time.Sleep(config.fetchTimeout)
				}
			default:
				log.Printf("no parser found for %s", conf.String())
			}
		default:
			log.Printf("no parser found for %s", conf.String())
		}
	}
}
