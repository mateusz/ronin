package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func main() {
	backend, err := url.Parse("http://localhost:80/")
	if err != nil {
		panic(err)
	}

	// Handle all asset requests directly.
	ronin, err := NewRonin("/etc/ronin.json")
	if err != nil {
		panic(err)
	}
	http.HandleFunc("/cwp-installer-clean/assets/", ronin.ServeHTTP)

	// Reverse proxy all the remaining requests.
	proxy := httputil.NewSingleHostReverseProxy(backend)
	http.HandleFunc("/", proxy.ServeHTTP)

	log.Fatal(http.ListenAndServe(":8080", nil))
}
