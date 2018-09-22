// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General
// Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program.  If not, see <http://www.gnu.org/licenses/>.

// Package api registers needed HTTP handlers.
package api

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"../store"
)

var (
	// Store should point to an implementation of TiddlerStore.
	StoreDb store.TiddlerStore

	Sess = NewSession()

	// Authenticate is a hook that lets the client of the package to provide authentication.
	Authenticate func(user string, pwd string) (bool)

	// ServeBase is a callback that should serve the index page.
	ServeBase = func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	}
)

func InitHandle(mux *Mux) {
	mux.HandleFunc("/", withLogging(index))
	mux.HandleFunc("/status", withLogging(status))
	mux.HandleFunc("/challenge/tiddlywebplugins.tiddlyspace.cookie_form", withLogging(login)) // POST, user=ee&password=11&tiddlyweb_redirect=%2Fstatus
	mux.HandleFunc("/logout", withLogging(logout)) // POST
	mux.HandleFunc("/recipes/all/tiddlers.json", withLogging(list))
	mux.HandleFunc("/recipes/all/tiddlers/", withLogging(tiddler))
	mux.HandleFunc("/bags/bag/tiddlers/", withLogging(remove))
}

// internalError logs err to the standard error and returns HTTP 500 Internal Server Error.
func internalError(w http.ResponseWriter, err error) {
	log.Println("ERR", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// logRequest logs the incoming request.
func logRequest(r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	log.Println(host, r.Method, r.URL, r.Referer(), r.UserAgent())
}

// withLogging is a logging middleware.
func withLogging(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		f(w, r)
	}
}

func checkAuth(w http.ResponseWriter, r *http.Request) (ok bool) {
	_, err := Sess.GetSID(r)
	if err != nil { // do not add cookie
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}

	sess, err := Sess.Start(w, r)
	if err != nil {
		internalError(w, err)
		return ok
	}

	if !sess.IsLogin() {
		Sess.Destroy(w, r)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return ok
	}
	return true
}

// index serves the index page.
func index(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "HEAD":
		return
	case "OPTIONS":
		w.Header().Add("Allow", "GET, HEAD, PUT, OPTIONS")
		w.Header().Add("DAV", "1, 2") // hack for WebDAV sync adaptor/saver
		return
	case "PUT":
		if !checkAuth(w, r) {
			return
		}

		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			internalError(w, err)
			return
		}
		err = ioutil.WriteFile("index.html", b, 0644)
		if err != nil {
			internalError(w, err)
			return
		}
		return
	default:
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	gzw := TryGzipResponse(w, r)
	defer gzw.Close()
	ServeBase(gzw, r)
}

func login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	user := r.Form.Get("user")
	pwd := r.Form.Get("password")

	if Authenticate != nil {
		ok := Authenticate(user, pwd)
		if ok {
			sess, err := Sess.Start(w, r)
			if err != nil {
				internalError(w, err)
				return
			}

			if sess.IsLogin() {
				return
			}
			sess.Login(user)
		}
	}
}

func logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	Sess.Destroy(w, r)
}

// status serves the status JSON.
func status(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gusetret := []byte(`{"username":"GUEST","space":{"recipe":"all"}}`)

	_, err := Sess.GetSID(r)
	if err != nil { // do not add cookie
		w.Header().Set("Content-Type", "application/json")
		w.Write(gusetret)
		return
	}

	sess, err := Sess.Start(w, r)
	if err != nil {
		internalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	uid, ok := sess.Get("uid")
	if ok {
		ret := fmt.Sprintf(`{"username":"%s","space":{"recipe":"all"}}`, uid)
		w.Write([]byte(ret))
	} else {
		Sess.Destroy(w, r)
		w.Write(gusetret)
	}
}

// list serves a JSON list of (mostly) skinny tiddlers.
func list(w http.ResponseWriter, r *http.Request) {
	_, err := Sess.GetSID(r)
	if err == nil { // renew session
		_, err := Sess.Start(w, r)
		if err != nil {
			internalError(w, err)
			return
		}
	}

	tiddlers, err := StoreDb.All(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	gzw := TryGzipResponse(w, r)
	defer gzw.Close()
	err = json.NewEncoder(gzw).Encode(tiddlers)
	if err != nil {
		log.Println("ERR", err)
	}
}

// getTiddler serves a fat tiddler.
func getTiddler(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/recipes/all/tiddlers/")

	t, err := StoreDb.Get(r.Context(), key)
	if err != nil {
		internalError(w, err)
		return
	}

	data, err := t.MarshalJSON()
	if err != nil {
		internalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if len(data) > 1024 {
		gzw := TryGzipResponse(w, r)
		defer gzw.Close()
		gzw.Write(data)
	} else {
		w.Write(data)
	}
}

// putTiddler saves a tiddler.
func putTiddler(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/recipes/all/tiddlers/")

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var js map[string]interface{}
	err = json.Unmarshal(buf, &js)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	js["bag"] = "bag"

	isSys := strings.HasPrefix(key, "$:/")
	isDraft := false
	fields, ok := js["fields"].(map[string]interface{})
	if ok {
		_, isDraft = fields["draft.of"]
	}

	rev, err := StoreDb.Put(r.Context(), store.Tiddler{
		//Meta: buf,

		Key:  key,
		IsDraft: isDraft,
		IsSys: isSys,

		Js: js,
	})
	if err != nil {
		internalError(w, err)
		return
	}

	etag := fmt.Sprintf(`"bag/%s/%d:%032x"`, url.QueryEscape(key), rev, md5.Sum([]byte(buf)))
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusNoContent)
}

func tiddler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getTiddler(w, r)
	case "PUT":
		if r.URL.Path == "/recipes/all/tiddlers/$:/StoryList" { // skip
			w.Header().Set("ETag", "bag/%24%3A%2FStoryList/0:00")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !checkAuth(w, r) {
			return
		}
		putTiddler(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// remove removes a tiddler.
func remove(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkAuth(w, r) {
		return
	}

	key := strings.TrimPrefix(r.URL.Path, "/bags/bag/tiddlers/")
	err := StoreDb.Delete(r.Context(), key)
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
