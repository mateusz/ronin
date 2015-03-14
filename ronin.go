package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	. "github.com/yvasiyarov/php_session_decoder"
	"io/ioutil"
	"net/http"
	"strings"
)

type Config struct {
	DB struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Hostname string `json:"hostname"`
	}
	AssetPath string `json:"asset-path"`
}

type Ronin struct {
	Config *Config
	DB     *sql.DB
}

func NewRonin(configPath string) (*Ronin, error) {

	jsonStr, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// Use field tags http://weekly.golang.org/pkg/encoding/json/#Marshal
	config := &Config{}
	err = json.Unmarshal(jsonStr, config)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s:%s@tcp(%s:3306)/?timeout=500ms", config.DB.Username, config.DB.Password, config.DB.Hostname)
	db, err := sql.Open("mysql", url)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	ronin := &Ronin{
		Config: config,
		DB:     db,
	}

	return ronin, nil
}

func getPhpSession(sessionId string) (PhpSession, error) {
	sessionFile := fmt.Sprintf("/var/tmp/sess_%s", sessionId)
	sessionBytes, err := ioutil.ReadFile(sessionFile)
	if err != nil {
		return nil, err
	}

	decoder := NewPhpDecoder(string(sessionBytes))
	session, err := decoder.Decode()
	if err != nil {
		return nil, err
	}

	return session, nil
}

// Copied from reverseproxy.go
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func (r *Ronin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	var session PhpSession
	if sessionCookie, err := req.Cookie("PHPSESSID"); err == nil {
		session, _ = getPhpSession(sessionCookie.Value)
	}

	if session != nil {
		// extract loggedInAs and find member by Member.ID
		fmt.Printf("%v", session["loggedInAs"])
	}

	filePath := singleJoiningSlash(r.Config.AssetPath, strings.TrimLeft(req.URL.Path, "/cwp-installer-clean/assets"))
	http.ServeFile(rw, req, filePath)
}
