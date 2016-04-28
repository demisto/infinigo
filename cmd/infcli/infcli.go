// Command line interface to CylanceV Infinity API
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/demisto/infinigo"
)

var (
	key        string
	url        string
	q          string
	f          string
	c          string
	jsonFormat bool
	v          bool
)

func init() {
	flag.StringVar(&key, "k", os.Getenv("INFINITY_KEY"), "The key to use for Infinity API access. Can be provided as an environment variable INFINITY_KEY.")
	flag.StringVar(&url, "url", infinigo.DefaultURL, "URL of the Infinity API to be used.")
	flag.StringVar(&q, "q", "", "hash or list of hashes separated by ',' for querying")
	flag.StringVar(&f, "f", "", "The file to upload for processing")
	flag.StringVar(&c, "c", "", "The confirmation code for the upload")
	flag.BoolVar(&jsonFormat, "json", false, "Should we print replies as JSON or formatted")
	flag.BoolVar(&v, "v", false, "Verbosity. If specified will trace the requests.")
}

func check(e error) {
	if e != nil {
		fmt.Fprintf(os.Stderr, "Error - %v\n", e)
		os.Exit(2)
	}
}

func main() {
	flag.Parse()
	if q == "" && f == "" {
		fmt.Fprintf(os.Stderr, "No command given. Please specify either q or f as parameters\n")
		os.Exit(1)
	}
	if f != "" && c == "" || c != "" && f == "" {
		fmt.Fprintf(os.Stderr, "You must provide both the file and confirmation code for upload\n")
		os.Exit(1)
	}
	inf, err := infinigo.New(infinigo.SetErrorLog(log.New(os.Stderr, "", log.Lshortfile)),
		infinigo.SetURL(url), infinigo.SetKey(key))
	check(err)
	if v {
		infinigo.SetTraceLog(log.New(os.Stderr, "", log.Lshortfile))(inf)
	}
	if q != "" {
		hashes := strings.Split(q, ",")
		res, err := inf.Query("", hashes...)
		check(err)
		if jsonFormat {
			b, err := json.MarshalIndent(res, "", "\t")
			check(err)
			fmt.Println(string(b))
		} else {
			for k, v := range res {
				score := "-"
				if v.GeneralScore != 0 {
					score = fmt.Sprintf("%v", v.GeneralScore)
				}
				confCode := "-"
				if v.ConfirmCode != "" {
					confCode = v.ConfirmCode
				}
				fmt.Printf("%s\t%s [%v] %s\t%v\t%s\t%v\n", k, v.Status, v.StatusCode, v.Error, score, confCode, v.Classifiers)
			}
		}
	}
	if f != "" {
		res, err := inf.UploadFile(c, f)
		check(err)
		if jsonFormat {
			b, err := json.MarshalIndent(res, "", "\t")
			check(err)
			fmt.Println(string(b))
		} else {
			for _, v := range res {
				fmt.Printf("Upload done with result: %s [%v] %s\n", v.Status, v.StatusCode, v.Error)
			}
		}
	}
}
