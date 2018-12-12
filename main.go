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
	timeoutMilliSeconds int
	conferencesFile     string
	outputDirectory     string
	conferences         []Conference
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

func getDownloadUrl(pageUrl string) (string, error) {
	response, err := http.Get(pageUrl)
	if err != nil {
		return "", err
	}

	root, err := html.Parse(response.Body)
	if err != nil {
		return "", err
	}

	// define a matcher
	matcher := func(n *html.Node) bool {
		// must check for nil values
		if n.DataAtom == atom.A && n.Parent != nil {
			return scrape.Attr(n.Parent, "class") == "file"
		}
		return false
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

// Pre-main bind flags to variables
func init() {
	flag.IntVar(&config.timeoutMilliSeconds, "timeout", 1000, "timeout between downloading papers")
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
			response, err := http.Get(conf.URL)
			if err != nil {
				log.Fatal(err)
			}

			root, err := html.Parse(response.Body)
			if err != nil {
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

			// grab all paper links
			pageNodes := scrape.FindAll(root, matcher)
			pages := make([]string, 0)
			for _, page := range pageNodes {
				url, err := getFullUrl(conf.URL, scrape.Attr(page, "href"))
				if err != nil {
					log.Fatal(err)
				}
				pages = append(pages, url)
			}

			// create conference directory
			confDirectory := path.Join(config.outputDirectory, conf.Name, strconv.Itoa(conf.Year))
			if _, err := os.Stat(confDirectory); os.IsNotExist(err) {
				if err := os.MkdirAll(confDirectory, os.ModePerm); err != nil {
					log.Fatal(err)
				}
			}

			for _, p := range pages {
				downloadUrl, err := getDownloadUrl(p)
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
				time.Sleep(500*time.Millisecond)
			}

		default:
			log.Printf("no parser found for %s", conf.String())
		}
	}
}
