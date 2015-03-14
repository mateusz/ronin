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

	// Get user security group memberships.
	if session != nil {
		if memberId, ok := session["loggedInAs"]; ok {
		}
	}

	// Get the Filename as used in the DB.
	relativePath := strings.TrimLeft(req.URL.Path, "/cwp-installer-clean")
	// Get the real filesystem path.
	filesystemPath := singleJoiningSlash(r.Config.AssetPath, strings.TrimLeft(relativePath, "/assets"))

	// Get file security settings.
	var id int
	var parentId int
	var canViewType string
	err := r.DB.QueryRow("select ID, ParentID, CanViewType from File where Filename='?'", relativePath).Scan(&id, &parentId, &canViewType)
	if err != nil {
		// This file is not managed by SilverStripe.
		rw.WriteHeader(http.StatusNotFound)
		return
	}

	// Walk up the inheritance tree to find the first object with normal permission.
	for canViewType == "Inherit" {
		if parentId == 0 {
			// We have reached the top and found we are inheriting all the way up. This means the file is open for viewing.
			http.ServeFile(rw, req, filesystemPath)
			return
		}

		err := r.DB.QueryRow("SELECT ID, ParentID, CanViewType FROM File where ID='?'", parentId).Scan(&id, &parentId, &canViewType)
		if err != nil {
			// Cannot establish the access permissions.
			rw.WriteHeader(http.StatusNotFound)
			return
		}
	}

	// We have found non-inheriting node, we can now check the permissions.
	if canViewType == "OnlyTheseUsers" {
		// If we can walk the following graph, the member is allowed: Member(memberId) -> Grou_Members -> Group -> File_ViewerGroups -> File(id)
		if memberId, ok := session["loggedInAs"]; ok {

			var count int
			err := r.DB.QueryRow(`
					SELECT count(File.ID)
					FROM Member
					RIGHT JOIN Group_Members
						ON Member.ID=Group_Members.MemberID
					RIGHT JOIN Group
						ON Group_Members.GroupID=Group.ID
					RIGHT JOIN File_ViewerGroups
						ON Group.ID=File_ViewerGroups.GroupID
					WHERE
						Member.ID='?'
						AND
						File_ViewerGroups.FileID='?'
				`, memberId, id).Scan(&count)

			if err != nil {
				// Cannot establish the access permissions.
				rw.WriteHeader(http.StatusNotFound)
				return
			}

			// TODO find out if the graph was indeed walkable :-)
			if graph was walkable {
				// The member is allowed to access the file.
				http.ServeFile(rw, req, filesystemPath)
				return
			}

		}
	} else if canViewType == "LoggedInUsers" {
		if _, ok := session["loggedInAs"]; ok {
			// Found an existing SilverStripe session, we can serve the file.
			http.ServeFile(rw, req, filesystemPath)
			return
		}
	}

	// Default to file not found.
	rw.WriteHeader(http.StatusNotFound)
	return
}
