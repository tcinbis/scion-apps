package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/netsec-ethz/scion-apps/_examples/flowtele/dbus/datalogger"
	"github.com/netsec-ethz/scion-apps/pkg/pan"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/netsec-ethz/scion-apps/pkg/shttp"
	"golang.org/x/net/html"
)

const (
	Bit  = 1
	KBit = 1000 * Bit
	MBit = 1000 * KBit
	GBit = 1000 * MBit

	Byte  = 8 * Bit
	KByte = 1000 * Byte
	MByte = 1000 * KByte
)

func main() {
	serverAddrStr := flag.String("s", "", "Server address (<ISD-AS,[IP]> or <hostname>, optionally with appended <:port>)")
	uriStr := flag.String("u", "", "URI to request from server.")
	fileEnding := flag.String("f", ".m4v", "Suffix to filter links by.")
	printBody := flag.Bool("p", false, "Whether to print the response's body to stdout.")
	flag.Parse()

	if len(*serverAddrStr) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	// Create a standard server with our custom RoundTripper
	c := &http.Client{
		Transport: shttp.NewRoundTripper(&tls.Config{InsecureSkipVerify: true}, nil),
	}

	query := fmt.Sprintf("https://%s/%s", *serverAddrStr, *uriStr)
	fmt.Printf("Requesting: %s\n", query)
	if *printBody {
		resp, err := c.Get(shttp.MangleSCIONAddrURL(query))
		if err != nil {
			log.Fatal("GET request failed: ", err)
		}
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))
		resp.Body.Close()
	}

	// fetch directory and extract links
	resp, err := c.Get(shttp.MangleSCIONAddrURL(query))
	if err != nil {
		log.Fatal("GET request failed: ", err)
	}
	defer resp.Body.Close()
	links := getLinks(resp.Body)
	sort.Strings(links)

	if len(links) < 1 {
		fmt.Println("No links to download found.")
		return
	}

	ctx, cancelLogger := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	bwLogger := datalogger.CreateBandwidthLogger(ctx, "bw", &wg)

	bufSize := 10000000
	localIA := pan.LocalIA().String()
	for _, l := range links {
		if strings.HasSuffix(l, *fileEnding) {
			// download it!
			query = fmt.Sprintf("https://%s/%s/%s", *serverAddrStr, *uriStr, l)

			resp, err := c.Get(query)
			if err != nil {
				log.Printf("GET request failed: %v", err)
				break
			}

			totalTime, totalBytes := 0.0, 0
			buf := make([]byte, bufSize)
			for {
				tStart := time.Now()
				n, err := io.ReadFull(resp.Body, buf)
				tDur := time.Now().Sub(tStart).Seconds()
				if err != nil {
					if err == io.EOF {
						// Finished streaming the file. Continue to next link.
						break
					}
					fmt.Printf("Error while receiving: %v\n", err)
					break
				}
				currentBW := float64(bufSize) * Byte / tDur / MBit
				bwLogger.Send(&datalogger.BandwidthData{
					LocalAddr:   localIA,
					Timestamp:   time.Now(),
					BWPerSecond: currentBW,
				})
				fmt.Printf("Current speed: %.2f MBit/s\n", currentBW)
				totalTime += tDur
				totalBytes += n
				if err == io.ErrUnexpectedEOF {
					break
				}
			}

			resp.Body.Close()
			fmt.Printf("Total for fetching %s with %.2f MBit/s\n", query, float64(totalBytes)*Byte/totalTime/MBit)
			time.Sleep(2 * time.Second)
		}
	}
	cancelLogger()
	wg.Wait()
}

func getLinks(body io.Reader) []string {
	var links []string
	z := html.NewTokenizer(body)
	for {
		tt := z.Next()

		switch tt {
		case html.ErrorToken:
			//todo: links list shoudn't contain duplicates
			return links
		case html.StartTagToken, html.EndTagToken:
			token := z.Token()
			if "a" == token.Data {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						links = append(links, attr.Val)
					}

				}
			}

		}
	}
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
