package gochimp3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"regexp"
	"time"

	"github.com/PathDNA/ptk"
)

// URIFormat defines the endpoint for a single app
const URIFormat string = "%s.api.mailchimp.com"

// Version the latest API version
const Version string = "/3.0"

// DatacenterRegex defines which datacenter to hit
var DatacenterRegex = regexp.MustCompile("[^-]\\w+$")

// API represents the origin of the API
type API struct {
	Key       string
	Timeout   time.Duration
	Transport http.RoundTripper

	User  string
	Debug bool

	endpoint string
}

// New creates a API
func New(apiKey string) *API {
	u := url.URL{}
	u.Scheme = "https"
	u.Host = fmt.Sprintf(URIFormat, DatacenterRegex.FindString(apiKey))
	u.Path = Version

	return &API{
		User:     "gochimp3",
		Key:      apiKey,
		endpoint: u.String(),
	}
}

func (api API) Request(method, path string, params QueryParams, body, response interface{}) error {
	// Retry 5 times every 2 minutes
	return ptk.RetryCtx(context.Background(), func() error {
		if err := api.rawRequest(method, path, params, body, response); err != nil {
			return err
		}

		return nil
	}, 5, time.Duration(Random(1, 10)*time.Minute), 0)
}

func Random(min, max int) time.Duration {
	return time.Duration(rand.Intn(max-min) + min)
}

// Request will make a call to the actual API.
func (api API) rawRequest(method, path string, params QueryParams, body, response interface{}) error {
	client := &http.Client{Transport: api.Transport}
	if api.Timeout > 0 {
		client.Timeout = api.Timeout
	}

	requestURL := fmt.Sprintf("%s%s", api.endpoint, path)
	if api.Debug {
		log.Printf("Requesting %s: %s\n", method, requestURL)
	}

	var bodyBytes io.Reader
	var err error
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
		bodyBytes = bytes.NewBuffer(data)
		if api.Debug {
			log.Printf("Adding body: %+v\n", body)
		}
	}

	req, err := http.NewRequest(method, requestURL, bodyBytes)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(api.User, api.Key)

	if params != nil && !reflect.ValueOf(params).IsNil() {
		queryParams := req.URL.Query()
		for k, v := range params.Params() {
			if v != "" {
				queryParams.Set(k, v)
			}
		}
		req.URL.RawQuery = queryParams.Encode()

		if api.Debug {
			log.Printf("Adding query params: %q\n", req.URL.Query())
		}
	}

	if api.Debug {
		dump, _ := httputil.DumpRequestOut(req, true)
		log.Printf("%s", string(dump))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if api.Debug {
		dump, _ := httputil.DumpResponse(resp, true)
		log.Printf("%s", string(dump))
	}

	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Do not unmarshall response is nil
		if response == nil || reflect.ValueOf(response).IsNil() || len(data) == 0 {
			return nil
		}

		err = json.Unmarshal(data, response)
		if err != nil {
			return err
		}

		return nil
	}

	// This is an API Error
	return parseAPIError(data)
}

// RequestOk Make Request ignoring body and return true if HTTP status code is 2xx.
func (api API) RequestOk(method, path string) (bool, error) {
	err := api.Request(method, path, nil, nil, nil)
	if err != nil {
		return false, err
	}
	return true, nil
}

func parseAPIError(data []byte) error {
	log.Println("MAILCHIMP ERROR", string(data))
	apiError := new(APIError)
	err := json.Unmarshal(data, apiError)
	if err != nil {
		return err
	}

	return apiError
}
