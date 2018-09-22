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

// widdly is a self-hosted web application which can serve as a personal TiddlyWiki.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"io/ioutil"

	"flag"
	"log"
	"bufio"
	"context"
	"crypto/tls"
	"crypto/sha256"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"strings"
	"time"


	"./api"
	"./store"
	_ "./store/bolt"
	_ "./store/sqlite"
	_ "./store/flatFile"

)

var (
	VERSION = "SELFBUILD" // injected by buildflags

	addr       = flag.String("server", "127.0.0.1:8080", "HTTP(S) service address")
	dataSource = flag.String("db", "widdly.db", "Database path/file")
	dataType   = flag.String("dbt", "flatFile", "Database type")

	crtFile    = flag.String("crt", "", "PEM encoded certificate file")
	keyFile    = flag.String("key", "", "PEM encoded private key file")
	genKey     = flag.Bool("genkey", false, "generate self-sign EC certificate")

	gziplv   = flag.Int("gz", 1, "gzip compress level, 0 for disable")
	rev   = flag.Int("rev", -1, "Max keeping history count, 0 for disable, -1 for unlimit")

	accounts   = flag.String("acc", "user.lst", "user list file")
	// eache line end with '\n': <user>\t<salt>\t<sha256(pwd)>
	// comment start with '#'

	user   = flag.String("u", "", "encode user name to user.lst format")
	pass   = flag.String("p", "", "encode user password to user.lst format")
)

func main() {
	flag.Parse()

	if *user != "" && *pass != "" {
		uid := *user
		salt := genSalt()
		hash := pwdHashStr(*pass, salt)

		fmt.Println("# user\tsalt\thash")
		fmt.Printf("%s\t%s\t%s\n", uid, salt, hash)
		return
	}

	if *genKey && *crtFile != "" && *keyFile != "" {
		fmt.Println("generate self-sign EC certificate...", *crtFile, *keyFile)
		genCert(*crtFile, *keyFile)
		fmt.Println("generate finish")
		return
	}

	fmt.Println("[server] version =", VERSION)
	fmt.Println("[server] gzip level =", *gziplv)
	fmt.Println("[server] max history count =", *rev)

	// read in accounts
	af, err := os.Open(*accounts)
	if err != nil {
		fmt.Println("[user] Open list error", err)
		return
	}

	userlist, err := readTSV(af)
	if err != nil {
		fmt.Println("[user] Parse list error", *accounts, err)
		return
	}
	fmt.Println("[user] count =", len(userlist))


	mux := api.NewRootMux()
	api.InitHandle(mux)

	// Open the data store and tell HTTP handlers to use it.
	db, err := store.Open(*dataType, *dataSource)
	if err != nil {
		list := store.ListBackend()
		fmt.Println("[backend] Open backend error", err)
		fmt.Println("[backend] list:", list)
		return
	}
	defer db.Close()
	db.SetMaxHistory(*rev)

	api.StoreDb = db
	api.GzipLevel = *gziplv

	api.Authenticate = func(user string, pwd string) (bool) {
		t0 := time.Now().Add(time.Second)
		defer time.Sleep(time.Until(t0)) // prevent brute force & timing attacks

		u, ok := userlist[user]
		if !ok {
			return false
		}

		hpwd := pwdHashStr(pwd, u.Salt)
		if hpwd == u.Hash {
			return true
		}
		return false
	}

	srv := &http.Server{Addr: *addr, Handler: mux}

	waitClosed := make(chan struct{})
	sigint := make(chan os.Signal, 1)
	go func() {
		signal.Notify(sigint, os.Interrupt, os.Kill, syscall.SIGTERM)
		<-sigint

		// received an interrupt signal, shutdown.
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			log.Printf("[server] shutdown: %v", err)
		}
		close(waitClosed)
	}()

	startServer(srv)

	select {
	case <-sigint:
	default:
		close(sigint)
	}
	<-waitClosed // block until server shutdown
}

func startServer(srv *http.Server) {
	var err error

	// check tls
	if *crtFile != "" && *keyFile != "" {
		cfg := &tls.Config{
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{

				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,

				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,

				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, // http/2 must
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, // http/2 must

				tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,

				tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,

				tls.TLS_RSA_WITH_AES_256_GCM_SHA384, // weak
				tls.TLS_RSA_WITH_AES_256_CBC_SHA, // waek
			},
		}
		srv.TLSConfig = cfg
		//srv.TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0) // disable http/2

		log.Printf("[server] HTTPS server Listen on: %v", *addr)
		err = srv.ListenAndServeTLS(*crtFile, *keyFile)
	} else {
		log.Printf("[server] HTTP server Listen on: %v", *addr)
		err = srv.ListenAndServe()
	}

	if err != http.ErrServerClosed {
		log.Printf("[server] ListenAndServe error: %v", err)
	}
}

func genCert(crtPath string, keyPath string) {
	//key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate ECDSA key: %s\n", err)
	}

	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatalf("Failed to serialize ECDSA key: %s\n", err)
	}

	keyPem := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDer,
	})
	err = ioutil.WriteFile(keyPath, keyPem, 0600)
	if err != nil {
		log.Fatalf("Failed to write '%s': %s", keyPath, err)
	}


	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 64)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		log.Fatalf("failed to generate serial number:", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"cs8425/widdly"},
			CommonName: "TiddlyWiki",
		},
		NotBefore: time.Now(),
		NotAfter: time.Now().AddDate(10, 0, 0), // 10 years
		//BasicConstraintsValid: true,
		//IsCA: true,
		//KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, // | x509.KeyUsageCertSign
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDer, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("Failed to create certificate: %s\n", err)
	}

	crtPem := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDer,
	})
	err = ioutil.WriteFile(crtPath, crtPem, 0600)
	if err != nil {
		log.Fatalf("Failed to write '%s': %s", crtPath, err)
	}

}

type User struct {
	UID            string
	Salt           string
	Hash           string
}

func readTSV(input io.ReadCloser) (map[string]*User, error) {
	defer input.Close()

	list := make(map[string]*User)
	r := bufio.NewReader(input)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		row := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
		if len(row) < 3 {
			continue
		}

		if row[0] == "" {
			continue
		}
		if strings.HasPrefix(row[0], "#") {
			continue
		}

		uid := row[0]
		salt := row[1]
		hash := row[2]

		list[uid] = &User{
			UID: uid,
			Salt: salt,
			Hash: hash,
		}
	}

	return list, nil
}

func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func genSalt() string {
	buf, err := generateRandomBytes(15)
	if err != nil {
		return ""
	}

	return base64.StdEncoding.EncodeToString(buf)
}

func hashBytes(a []byte) []byte {
	shah := sha256.New()
	shah.Write(a)
	return shah.Sum([]byte(""))
}

func pwdHash(pwd string, salt string) []byte {
	return hashBytes([]byte(pwd + "-:-" + salt))
}

func pwdHashStr(pwd string, salt string) string {
	return hex.EncodeToString(pwdHash(pwd, salt))
}

