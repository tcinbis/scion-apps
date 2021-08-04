package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/netsec-ethz/scion-apps/pkg/shttp"
)

func main() {
	serverAddrStr := flag.String("s", "", "Server address (<ISD-AS,[IP]> or <hostname>, optionally with appended <:port>)")
	uriStr := flag.String("u", "", "URI to request from server.")
	flag.Parse()

	if len(*serverAddrStr) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	// Create a standard server with our custom RoundTripper
	c := &http.Client{
		Transport: shttp.NewRoundTripper(nil, &tls.Config{InsecureSkipVerify: true}, nil),
	}
	// (just for demonstration on how to use Close. Clients are safe for concurrent use and should be re-used)
	defer c.Transport.(*shttp.RoundTripper).Close()

	// Make a get request
	start := time.Now()
	query := fmt.Sprintf("https://%s/%s", *serverAddrStr, *uriStr)
	resp, err := c.Get(shttp.MangleSCIONAddrURL(query))
	if err != nil {
		log.Fatal("GET request failed: ", err)
	}
	defer resp.Body.Close()
	end := time.Now()

	log.Printf("\nGET request succeeded in %v seconds", end.Sub(start).Seconds())
	printResponse(resp)

	// Set Policy: stupid example just to show that it works, re-initialize the
	// interactive selection so it will prompt again, just to show that it works:
	//c.Transport.(*shttp.RoundTripper).SetPolicy(
	//	&pan.InteractiveSelection{
	//		Prompter: pan.CommandlinePrompter{},
	//	},
	//)
}

func printResponse(resp *http.Response) {
	fmt.Println("\n***Printing Response***")
	fmt.Println("Status: ", resp.Status)
	fmt.Println("Protocol:", resp.Proto)
	fmt.Println("Content-Length: ", resp.ContentLength)
	fmt.Println("Content-Type: ", resp.Header.Get("Content-Type"))
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Print(err)
	}
	if len(body) == 0 {
		fmt.Println("!Received empty body!")
	}
}
