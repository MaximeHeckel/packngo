package packngo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"crypto/tls"
)

const (
	libraryVersion = "0.1.0"
	baseURL        = "https://api.packet.net/"
	userAgent      = "packngo/" + libraryVersion
	mediaType      = "application/json"

	headerRateLimit     = "X-RateLimit-Limit"
	headerRateRemaining = "X-RateLimit-Remaining"
	headerRateReset     = "X-RateLimit-Reset"
)

// ListOptions specifies optional global API parameters
type ListOptions struct {
	// for paginated result sets, page of results to retrieve
	Page int `url:"page,omitempty"`

	// for paginated result sets, the number of results to return per page
	PerPage int `url:"per_page,omitempty"`

	// specify which resources you want to return as collections instead of references
	Includes string
}

type Response struct {
	*http.Response
	Rate
}
func (r *Response) populateRate() {
	// parse the rate limit headers and populate Response.Rate
	if limit := r.Header.Get(headerRateLimit); limit != "" {
		r.Rate.RequestLimit, _ = strconv.Atoi(limit)
	}
	if remaining := r.Header.Get(headerRateRemaining); remaining != "" {
		r.Rate.RequestsRemaining, _ = strconv.Atoi(remaining)
	}
	if reset := r.Header.Get(headerRateReset); reset != "" {
		if v, _ := strconv.ParseInt(reset, 10, 64); v != 0 {
			r.Rate.Reset = Timestamp{time.Unix(v, 0)}
		}
	}
}

type ErrorResponse struct {
	Response *http.Response
	Message string
}
func (r *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %v: %d %v",
		r.Response.Request.Method, r.Response.Request.URL, r.Response.StatusCode, r.Message)
}

// the base API Client
type Client struct {
	client *http.Client

	BaseURL *url.URL

	UserAgent string
	ConsumerToken string
	ApiKey string

	RateLimit Rate

	// Packet Api Objects
	Plans            PlanService
	Users            UserService
	Emails           EmailService
	SshKeys          SshKeyService
	Devices          DeviceService
	Projects         ProjectService
	Facilities       FacilityService
	OperatingSystems OSService
}

func (c *Client) NewRequest(method, path string, body interface{}) (*http.Request, error) {
	// relative path to append to the endpoint url, no leading slash please
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	u := c.BaseURL.ResolveReference(rel)

	// json encode the request body, if any
	buf := new(bytes.Buffer)
	if body != nil {
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, u.String(), buf)
	if err != nil {
		return nil, err
	}

	req.Close = true

	req.Header.Add("X-Auth-Token", c.ApiKey)
	req.Header.Add("X-Consumer-Token", c.ConsumerToken)

	req.Header.Add("Content-Type", mediaType)
	req.Header.Add("Accept", mediaType)
	req.Header.Add("User-Agent", userAgent)
	return req, nil
}

func (c *Client) Do(req *http.Request, v interface{}) (*Response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	response := Response{Response: resp}
	response.populateRate()
	c.RateLimit = response.Rate

	err = CheckResponse(resp)
	// if the response is an error, return the ErrorReponse
	if err != nil {
		return &response, err
	}

	if v != nil {
		// if v implements the io.Writer interface, return the raw response
		if w, ok := v.(io.Writer); ok {
			io.Copy(w, resp.Body)
		} else {
			err = json.NewDecoder(resp.Body).Decode(v)
			if err != nil {
				return &response, err
			}
		}
	}

	return &response, err
}

// initializes and returns a Client, use this to get an API Client to operate on
func NewClient(consumerToken string, apiKey string) *Client {
	httpClient := http.DefaultClient

	BaseURL, _ := url.Parse(baseURL)

	c := &Client{client: httpClient, BaseURL: BaseURL, UserAgent: userAgent, ConsumerToken: consumerToken, ApiKey: apiKey}
	c.Plans = &PlanServiceOp{client: c}
	c.Users = &UserServiceOp{client: c}
	c.Emails = &EmailServiceOp{client: c}
	c.SshKeys = &SshKeyServiceOp{client: c}
	c.Devices = &DeviceServiceOp{client: c}
	c.Projects = &ProjectServiceOp{client: c}
	c.Facilities = &FacilityServiceOp{client: c}
	c.OperatingSystems = &OSServiceOp{client: c}

	// THIS IS VERY VERY BAD, WE NEED TO FIX THE CERT ON THE SERVER
	// RELEVANT ERROR IS:
	// x509: certificate signed by unknown authority (possibly because of "x509: cannot verify signature: algorithm unimplemented" while trying to verify candidate authority certificate "COMODO RSA Certification Authority")
	cfg := &tls.Config{ InsecureSkipVerify: true }
	http.DefaultClient.Transport = &http.Transport{
    TLSClientConfig: cfg,
	}
	// END BAD PART

	return c
}

func CheckResponse(r *http.Response) error {
	// return if http status code is within 200 range
	if c := r.StatusCode; c >= 200 && c <= 299 {
		return nil
	}

	errorResponse := &ErrorResponse{Response: r}
	data, err := ioutil.ReadAll(r.Body)
	// if the response has a body, populate the message in errorResponse
	if err == nil && len(data) > 0 {
		json.Unmarshal(data, errorResponse)
	}

	return errorResponse
}
