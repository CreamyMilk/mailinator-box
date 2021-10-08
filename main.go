package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"golang.org/x/net/websocket"
)

var (
	DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/94.0.4606.61 Safari/537.36"
	DefaultHostname  = "www.mailinator.com"
)

type Config struct {
	Agent    string
	Hostname string
}

type Inbox struct {
	mailReceiver  chan *Message
	conf          *Config
	sessionCookie string
	inboxName     string
	ws            *websocket.Conn
	stop          chan bool
	err           error
}

type Message struct {
	From string `json:"from"`
	To   string `json:"to"`
	Data struct {
		Fromfull string `json:"fromfull"`
		Headers  struct {
			Date             string `json:"date"`
			From             string `json:"from"`
			Contenttype      string `json:"contenttype"`
			Dkimsignature    string `json:"dkimsignature"`
			Mimeversion      string `json:"mimeversion"`
			Subject          string `json:"subject"`
			Feedbackid       string `json:"feedbackid"`
			Messageid        string `json:"messageid"`
			Received         string `json:"received"`
			To               string `json:"to"`
			XSessionoutgoing string `json:"xSessionoutgoing"`
		} `json:"headers"`
		Subject    string `json:"subject"`
		RequestID  string `json:"requestId"`
		Origfrom   string `json:"origfrom"`
		Id         string `json:"id"`
		Time       int    `json:"time"`
		SecondsAgo int    `json:"seconds_ago"`
		Parts      []struct {
			Headers struct {
				Contenttype             string `json:"contenttype"`
				Contenttransferencoding string `json:"contenttransferencoding"`
			} `json:"headers"`
			Body string `json:"body"`
		} `json:"parts"`
	} `json:"data"`
}

func New(inboxName string, conf *Config) (*Inbox, error) {
	if conf == nil {
		conf = &Config{
			Agent:    DefaultUserAgent,
			Hostname: DefaultHostname,
		}
	}
	in := &Inbox{
		mailReceiver: make(chan *Message),
		stop:         make(chan bool),
		inboxName:    inboxName,
		conf:         conf,
	}
	err := in.getSessionInfo()
	if err != nil {
		return nil, err
	}
	if err = in.createSocket(); err != nil {
		return nil, err
	}
	go in.run()
	return in, nil
}

func (in *Inbox) Close() error {
	close(in.stop)
	return in.err
}

func (in *Inbox) Error() error {
	return in.err
}
func (in *Inbox) getSessionInfo() error {
	resp, err := http.Get("https://" + in.conf.Hostname)
	if err != nil {
		return fmt.Errorf("session creation error [%s] ", err)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "JSESSIONID" {
			in.sessionCookie = c.Value
			break
		}
	}
	if in.sessionCookie == "" {
		return fmt.Errorf("seems the headers dont have a JSESSION cookie in HTTP response")
	}
	return nil
}

func (in *Inbox) createSocket() error {
	h := http.Header{}
	h.Add("User-Agent", in.conf.Agent)
	h.Add("Cookie", fmt.Sprintf("JSESSIONID=%s", in.sessionCookie))
	c := &websocket.Config{
		Location: in.parseurl("wss://%s/ws/fetchpublic", in.conf.Hostname),
		Origin:   in.parseurl("https://%s", in.conf.Hostname),
		Header:   h,
		Version:  13,
	}
	if in.err != nil {
		return in.err
	}

	//Creates Websock connection here
	in.ws, in.err = websocket.DialConfig(c)
	return in.err
}

func (in *Inbox) fetchFullMessage(name string) (*http.Response, error) {
	log.Println("Getting data for id :", name)
	h := http.Header{}
	h.Add("User-Agent", in.conf.Agent)
	h.Add("Cookie", fmt.Sprintf("JSESSIONID=%s", in.sessionCookie))
	r, err := http.NewRequest("GET", name, nil)
	if err != nil {
		return nil, fmt.Errorf("get: %s", err)
	}
	return http.DefaultClient.Do(r)
}

func (in *Inbox) parseurl(fm string, arg ...interface{}) (u *url.URL) {
	u, in.err = url.Parse(fmt.Sprintf(fm, arg...))
	return u
}

func (in *Inbox) formatMessageUrl(id interface{}) string {
	return fmt.Sprintf("https://%s/fetch_public?msgid=%s", in.conf.Hostname, id)
}
func (in *Inbox) run() {
	var (
		data = make(map[string]interface{})
		resp *http.Response
	)
	x := map[string]string{
		"cmd":     "sub",
		"channel": in.inboxName,
	}
	err := websocket.JSON.Send(in.ws, x)
	if err != nil {
		log.Printf("Sending Websocket has failed badly, Heres why %s", err.Error())
	}

	defer close(in.mailReceiver)
	in.stop = make(chan bool)
	for in.err == nil {
		select {
		default:
			delete(data, "id")
			in.err = websocket.JSON.Receive(in.ws, &data)
			//log.Printf("The returned data is %+v \n", data)
			//Future me : You can get past inbox items here
			id, ok := data["id"]
			if !ok {
				continue
			}

			resp, in.err = in.fetchFullMessage(in.formatMessageUrl(id))
			if in.err != nil {
				return
			}

			msg := Message{}
			json.NewDecoder(resp.Body).Decode(&msg)
			if len(msg.Data.Parts) == 0 {
				continue
			}
			in.mailReceiver <- &msg
		case <-in.stop:
			return
		}
	}
}

func main() {
	targetInbox := "test"

	inbox, err := New(targetInbox, nil)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Hello 👋 my friend , am waiting for new messages sent\nTo: %s@mailinator.com", targetInbox)

	for msg := range inbox.mailReceiver {
		fmt.Println(msg)
	}
}
