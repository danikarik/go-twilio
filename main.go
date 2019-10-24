package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	twilioBaseURL               = "https://verify.twilio.com/v2/Services/%s"
	phoneVerificationRequestURL = "/Verifications"
	phoneVerificationCheckURL   = "/VerificationCheck"
)

type envError struct{ Key string }

func (e envError) Error() string { return fmt.Sprintf("[%s] is not present", e.Key) }

func envLookup(keys ...string) (map[string]string, error) {
	envs := make(map[string]string)
	for _, key := range keys {
		val, ok := os.LookupEnv(key)
		if !ok {
			return envs, envError{key}
		}
		envs[key] = val
	}
	return envs, nil
}

type verificationResponse struct {
	SID        string    `json:"sid"`
	ServiceSID string    `json:"service_sid"`
	AccountSID string    `json:"account_sid"`
	To         string    `json:"to"`
	Channel    string    `json:"channel"`
	Status     string    `json:"status"`
	Valid      bool      `json:"valid"`
	CreatedAt  time.Time `json:"date_created"`
	UpdatedAt  time.Time `json:"date_updated"`
}

type twilio struct {
	ServiceSID string
	AccountSID string
	AuthToken  string
}

func (t *twilio) methodURL(path string) (*url.URL, error) {
	url, err := url.Parse(fmt.Sprintf(twilioBaseURL, t.ServiceSID) + path)
	if err != nil {
		return nil, err
	}

	return url, nil
}

func (t *twilio) doRequest(method string, u *url.URL) ([]byte, error) {
	r, err := http.NewRequest(method, u.String(), strings.NewReader(u.RawQuery))
	if err != nil {
		return nil, err
	}
	r.SetBasicAuth(t.AccountSID, t.AuthToken)
	r.Header.Add("Accept", "application/json")
	r.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, fmt.Errorf("got wrong status code: %d", resp.StatusCode)
	}

	return data, nil
}

func (t *twilio) requestCode(w http.ResponseWriter, r *http.Request) {
	type request struct {
		To      string `json:"to"`
		Channel string `json:"channel"`
	}

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var payload request
	err = json.Unmarshal(data, &payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	requestURL, err := t.methodURL(phoneVerificationRequestURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	v := url.Values{}
	v.Set("To", payload.To)
	v.Set("Channel", payload.Channel)

	requestURL.RawQuery = v.Encode()

	buf, err := t.doRequest("POST", requestURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var response verificationResponse
	err = json.Unmarshal(buf, &response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(response)
}

func (t *twilio) verifyCode(w http.ResponseWriter, r *http.Request) {
	type request struct {
		To   string `json:"to"`
		Code string `json:"code"`
	}

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var payload request
	err = json.Unmarshal(data, &payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	requestURL, err := t.methodURL(phoneVerificationCheckURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	v := url.Values{}
	v.Set("To", payload.To)
	v.Set("Code", payload.Code)

	requestURL.RawQuery = v.Encode()

	buf, err := t.doRequest("POST", requestURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var response verificationResponse
	err = json.Unmarshal(buf, &response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !response.Valid {
		http.Error(w, "not valid", http.StatusNotAcceptable)
		return
	}

	json.NewEncoder(w).Encode(response)
}

func (t *twilio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("/request", t.requestCode)
	mux.HandleFunc("/verify", t.verifyCode)
	mux.ServeHTTP(w, r)
}

func main() {
	envs, err := envLookup(
		"TWILIO_SERVICE_SID",
		"TWILIO_ACCOUNT_SID",
		"TWILIO_TOKEN",
	)
	if err != nil {
		log.Fatal(err)
	}

	twilio := &twilio{
		ServiceSID: envs["TWILIO_SERVICE_SID"],
		AccountSID: envs["TWILIO_ACCOUNT_SID"],
		AuthToken:  envs["TWILIO_TOKEN"],
	}

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      twilio,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	log.Println("Start listening on", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
