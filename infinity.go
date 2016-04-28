/*
Package infinigo is a library implementing the CylanceV Infinity API v2.0

Written by Slavik Markovich at Demisto
*/
package infinigo

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultURL          = "https://api.cylance.com/apiv2/" // DefaultURL is the URL for the API endpoint
	AuthHeader          = "X-IAUTH"                        // AuthHeader for the API key
	ContentTypeHeader   = "Content-Type"                   // Header for Content-Type
	ContentLengthHeader = "Content-Length"                 // Header for Content-Length
	GzipContentType     = "application/xgzip"
)

// Error structs are returned from this library for known error conditions
type Error struct {
	ID      string `json:"id"`      // ID of the error
	Details string `json:"details"` // Details of the error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.ID, e.Details)
}

var (
	// ErrMissingCredentials is returned when API key is missing
	ErrMissingCredentials = &Error{ID: "missing_credentials", Details: "You must provide the Infinity API key"}
)

// Client interacts with the services provided by Infinity.
type Client struct {
	key      string       // The API key
	url      string       // Infinity URL
	errorlog *log.Logger  // Optional logger to write errors to
	tracelog *log.Logger  // Optional logger to write trace and debug data to
	c        *http.Client // The client to use for requests
}

// OptionFunc is a function that configures a Client.
// It is used in New
type OptionFunc func(*Client) error

// errorf logs to the error log.
func (c *Client) errorf(format string, args ...interface{}) {
	if c.errorlog != nil {
		c.errorlog.Printf(format, args...)
	}
}

// tracef logs to the trace log.
func (c *Client) tracef(format string, args ...interface{}) {
	if c.tracelog != nil {
		c.tracelog.Printf(format, args...)
	}
}

// New creates a new CylanceV Infinity client.
//
// The caller can configure the new client by passing configuration options to the func.
//
// Example:
//
//   client, err := infinigo.New(
//     infinigo.SetKey("some key"),
//     infinigo.SetUrl("https://some.url.com:port/"),
//     infinigo.SetErrorLog(log.New(os.Stderr, "Cylance: ", log.Lshortfile))
//
// If no URL is configured, Client uses DefaultURL by default.
//
// If no HttpClient is configured, then http.DefaultClient is used.
// You can use your own http.Client with some http.Transport for advanced scenarios.
//
// An error is also returned when some configuration option is invalid.
func New(options ...OptionFunc) (*Client, error) {
	// Set up the client
	c := &Client{
		url: DefaultURL,
		c:   http.DefaultClient,
	}

	// Run the options on it
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}
	c.tracef("Using URL [%s]\n", c.url)

	if c.key == "" {
		c.errorf("Missing credentials")
		return nil, ErrMissingCredentials
	}
	return c, nil
}

// Initialization functions

// SetKey sets the Infinity API key
// To receive a key, please contact support@cylance.com
func SetKey(key string) OptionFunc {
	return func(c *Client) error {
		if key == "" {
			c.errorf("%v\n", ErrMissingCredentials)
			return ErrMissingCredentials
		}
		c.key = key
		return nil
	}
}

// SetHTTPClient can be used to specify the http.Client to use when making
// HTTP requests to Infinity API.
func SetHTTPClient(httpClient *http.Client) OptionFunc {
	return func(c *Client) error {
		if httpClient != nil {
			c.c = httpClient
		} else {
			c.c = http.DefaultClient
		}
		return nil
	}
}

// SetURL defines the URL endpoint for Infinity
func SetURL(rawurl string) OptionFunc {
	return func(c *Client) error {
		if rawurl == "" {
			rawurl = DefaultURL
		}
		u, err := url.Parse(rawurl)
		if err != nil {
			c.errorf("Invalid URL [%s] - %v\n", rawurl, err)
			return err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			err := &Error{ID: "bad_url", Details: fmt.Sprintf("Invalid schema specified [%s]", rawurl)}
			c.errorf("%v", err)
			return err
		}
		c.url = rawurl
		if !strings.HasSuffix(c.url, "/") {
			c.url += "/"
		}
		return nil
	}
}

// SetErrorLog sets the logger for critical messages. It is nil by default.
func SetErrorLog(logger *log.Logger) func(*Client) error {
	return func(c *Client) error {
		c.errorlog = logger
		return nil
	}
}

// SetTraceLog specifies the logger to use for output of trace messages like
// HTTP requests and responses. It is nil by default.
func SetTraceLog(logger *log.Logger) func(*Client) error {
	return func(c *Client) error {
		c.tracelog = logger
		return nil
	}
}

// dumpRequest dumps a request to the debug logger if it was defined
func (c *Client) dumpRequest(req *http.Request) {
	if c.tracelog != nil {
		out, err := httputil.DumpRequestOut(req, false)
		if err == nil {
			c.tracef("%s\n", string(out))
		}
	}
}

// dumpResponse dumps a response to the debug logger if it was defined
func (c *Client) dumpResponse(resp *http.Response) {
	if c.tracelog != nil {
		out, err := httputil.DumpResponse(resp, true)
		if err == nil {
			c.tracef("%s\n", string(out))
		}
	}
}

// Request handling functions

// handleError will handle responses with status code different from success
func (c *Client) handleError(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if c.errorlog != nil {
			out, err := httputil.DumpResponse(resp, true)
			if err == nil {
				c.errorf("%s\n", string(out))
			}
		}
		msg := fmt.Sprintf("Unexpected status code: %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
		c.errorf(msg)
		return &Error{ID: "http_error", Details: msg}
	}
	return nil
}

// do executes the API request.
// Returns the response if the status code is between 200 and 299
// `body` is an optional body for the POST requests.
func (c *Client) do(method, rawurl string, params map[string]string, body io.Reader, bodyLength int, result interface{}) error {
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Add(k, v)
		}
		rawurl += "?" + values.Encode()
	}

	req, err := http.NewRequest(method, c.url+rawurl, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(AuthHeader, c.key)
	if body != nil {
		req.Header.Set(ContentTypeHeader, GzipContentType)
		req.Header.Set(ContentLengthHeader, strconv.Itoa(bodyLength))
	}
	var t time.Time
	if c.tracelog != nil {
		c.dumpRequest(req)
		t = time.Now()
		c.tracef("Start request %s at %v", rawurl, t)
	}
	resp, err := c.c.Do(req)
	if c.tracelog != nil {
		c.tracef("End request %s at %v - took %v", rawurl, time.Now(), time.Since(t))
	}
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	if err = c.handleError(resp); err != nil {
		return err
	}
	c.dumpResponse(resp)
	if result != nil {
		switch result := result.(type) {
		// Should we just dump the response body
		case io.Writer:
			if _, err = io.Copy(result, resp.Body); err != nil {
				return err
			}
		default:
			if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
				if c.errorlog != nil {
					out, err := httputil.DumpResponse(resp, true)
					if err == nil {
						c.errorf("%s\n", string(out))
					}
				}
				return err
			}
		}
	}
	return nil
}

// Structs for responses

type Common struct {
	Status     string  `json:"status"`     // Status from Infinity API
	StatusCode float32 `json:"statuscode"` // StatusCode from Infinity API
	Error      string  `json:"error"`      // Error reason for Infinity API error
}

// QueryResponse for the query API endpoint
type QueryResponse struct {
	Common
	GeneralScore float32            `json:"generalscore"` // GeneralScore of the requested hash
	ConfirmCode  string             `json:"confirmcode"`  // If a file is requested to provide answer
	Classifiers  map[string]float32 `json:"classifiers"`  // If classifiers are requested, provide a score per classifier
}

type UploadResponse struct {
	Common
}

// Public API functions

// Query the Infinity API for a given list of endpoints
// If classifier is not provided, "all" will be selected. Options are none, ml, industry, human, all.
// Hashes can be any MD5, SHA1 and SHA256
func (c *Client) Query(classifiers string, hash ...string) (resp map[string]QueryResponse, err error) {
	if len(hash) == 0 {
		return nil, &Error{ID: "missing_arg", Details: "hash is required"}
	}
	if classifiers == "" {
		classifiers = "all"
	}
	resp = make(map[string]QueryResponse)
	err = c.do("GET", "q", map[string]string{"c": classifiers, "h": strings.Join(hash, ",")}, nil, 0, &resp)
	return
}

// Upload a file to Infinity API
func (c *Client) Upload(confirmCode string, data io.Reader) (resp map[string]UploadResponse, err error) {
	if confirmCode == "" {
		return nil, &Error{ID: "missing_arg", Details: "Confirmation code is required"}
	}
	if data == nil {
		return nil, &Error{ID: "missing_arg", Details: "Data is required"}
	}
	// Looks like Infinity API is really particular regarding the content length so need to actually specify it
	// and cannot stream the body - bad for memory but current workaround
	buf := &bytes.Buffer{}
	gw := gzip.NewWriter(buf)
	defer gw.Close()
	if _, err = io.Copy(gw, data); err != nil {
		return
	}
	resp = make(map[string]UploadResponse)
	err = c.do("PUT", "u/"+confirmCode, nil, buf, buf.Len(), &resp)
	return
}

// UploadFile to the Infinity API
func (c *Client) UploadFile(confirmCode, path string) (resp map[string]UploadResponse, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	return c.Upload(confirmCode, f)
}
