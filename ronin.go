package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	. "github.com/yvasiyarov/php_session_decoder"
	"io/ioutil"
	"log"
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

	url := fmt.Sprintf("%s:%s@tcp(%s:3306)/SS_cwp?timeout=500ms", config.DB.Username, config.DB.Password, config.DB.Hostname)
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

	// Get the Filename as used in the DB.
	relativePath := strings.TrimPrefix(req.URL.Path, "/cwp-installer-clean/")
	// Get the real filesystem path.
	filesystemPath := singleJoiningSlash(r.Config.AssetPath, strings.TrimPrefix(relativePath, "assets"))

	// Get file security settings.
	var fileId int
	var parentId int
	var canViewType string
	err := r.DB.QueryRow("select ID, ParentID, CanViewType from File where Filename=?", relativePath).Scan(&fileId, &parentId, &canViewType)
	if err == sql.ErrNoRows {
		// This file is not managed by SilverStripe.
		rw.WriteHeader(http.StatusNotFound)
		log.Printf("%s not found in the DB", relativePath)
		return
	}
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Found file with ID=%s and %s permission", fileId, canViewType)

	// Walk up the inheritance tree to find the first object with normal permission.
	for canViewType == "Inherit" {
		if parentId == 0 {
			// We have reached the top and found we are inheriting all the way up. This means the file is open for viewing.
			log.Printf("Found no more parents, while applying Inherit", parentId)
			http.ServeFile(rw, req, filesystemPath)
			return
		}

		err := r.DB.QueryRow("SELECT ID, ParentID, CanViewType FROM File where ID=?", parentId).Scan(&fileId, &parentId, &canViewType)
		if err == sql.ErrNoRows {
			// Cannot establish the access permissions.
			log.Printf("Parent folder with ID=%s not found, while applying Inherit", parentId)
			rw.WriteHeader(http.StatusForbidden)
			return
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("After walking up, found %s permission on File ID=%s", canViewType, fileId)

	// Find out if the session represents a logged in user.
	if _, ok := session["loggedInAs"]; !ok {
		log.Printf("No session detected, while applying OnlyTheseUsers")
		rw.WriteHeader(http.StatusForbidden)
		return
	}

	memberId := session["loggedInAs"]
	log.Printf("This user appears to be Member of ID=%s", memberId)

	// We have found non-inheriting node, we can now check the permissions.
	if canViewType == "OnlyTheseUsers" {
		// If we can walk the following graph, the member is allowed:
		// Member(memberId) -> Group_Members -> Group -> File_ViewerGroups -> File(fileId)

		var count int
		err := r.DB.QueryRow(`
				SELECT FileID
				FROM Member
				RIGHT JOIN Group_Members
					ON Member.ID=Group_Members.MemberID
				RIGHT JOIN `+"`Group`"+`
					ON Group_Members.GroupID=Group.ID
				RIGHT JOIN File_ViewerGroups
					ON Group.ID=File_ViewerGroups.GroupID
				WHERE
					Member.ID=?
					AND
					File_ViewerGroups.FileID=?
			`, memberId, fileId).Scan(&count)

		if err == sql.ErrNoRows {
			// Cannot establish the access permissions.
			log.Printf("Member ID=%s seems not to have a permission for File ID=%s, while applying OnlyTheseUsers", memberId, fileId)
			rw.WriteHeader(http.StatusForbidden)
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		http.ServeFile(rw, req, filesystemPath)
		return

	} else if canViewType == "LoggedInUsers" {
		// Found an existing SilverStripe session, we can serve the file.
		http.ServeFile(rw, req, filesystemPath)
		return

	}

	// Default to file not found.
	log.Printf("Default not found case")
	rw.WriteHeader(http.StatusNotFound)
	return
}
