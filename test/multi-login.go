package main

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

func main() {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// login once
	resp, err := client.PostForm("https://www.vidlii.com/login", url.Values{
		"username": {"bennyc"},
		"password": {"abc123456"},
	})
	if err != nil || resp.StatusCode != 200 {
		fmt.Printf("login failed: %v %v\n", err, resp.StatusCode)
		return
	}
	resp.Body.Close()
	fmt.Println("logged in")

	// fire 5 concurrent req with same session
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r, err := client.Get("https://www.vidlii.com/watch?v=K5u6i67Cg3e")
			if err != nil {
				fmt.Printf("[%d] error: %v\n", id, err)
				return
			}
			defer r.Body.Close()
			fmt.Printf("[%d] status: %d\n", id, r.StatusCode)
		}(i)
	}
	wg.Wait()
}
