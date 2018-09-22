widdly [![License](http://img.shields.io/:license-gpl3-blue.svg)](http://www.gnu.org/licenses/gpl-3.0.html) [![Build Status](https://travis-ci.org/opennota/widdly.png?branch=master)](https://travis-ci.org/opennota/widdly)
======

This is a minimal self-hosted app, written in Go, that can serve as a backend
for a personal [TiddlyWiki](http://tiddlywiki.com/).

## Requirements

Go 1.8+

## Build

get source

    $ git clone --depth=1 https://github.com/cs8425/widdly.git
    $ cd widdly

(optional) get dependency

    $ go get go.etcd.io/bbolt # bolt/bbolt support, cross-compile can work
    $ go get github.com/mattn/go-sqlite3 # sqlite support, won't (or hard to) work for cross-compile

build:

    $ go build .

or

    $ ./build_all.sh # build multi-arch executable binary to bin/widdly.*


## Usage

Setup account:

    ./widdly -u $user1 -p $pass1 > user.lst   # user 1, login name = $user1, password = $pass1
    ./widdly -u $user2 -p $pass2 >> user.lst  # user 2, login name = $user2, password = $pass2
                                             ...
    ./widdly -u $userN -p $passN >> user.lst  # user N, login name = $userN, password = $passN


Generate self-sign TLS EC Certificate & Key (optional):

    ./widdly -genkey -crt <server.crt> -key <server-priv.key>


Run:

    ./widdly -server :1337 -acc user.lst -db /path/to/the/database -gz 5 -rev 3

- `-server :1337` - listen on port 1337 (by default port 8080 on localhost)
- `-acc user.lst` - user list file.
- `-db /path/to/the/database` - explicitly specify which file to use for the database (by default `widdly.db` in the current directory)
- `-dbt flatFile` - database type: flatFile, bbolt, sqlite; use `-dbt ''` to list all
- `-gz 5` - gzip compress level (1~9), 0 for disable, -1 for golang default level
- `-rev n` - max keeping history count, 0 for disable, -1 for unlimit; which n >= 1 will use more n+1 disk space, total size = size_of(tiddler) * (n + 2)
- `-crt <crt.pem>`, `-key <key.pem>` - PEM encoded certificate file and private key file for HTTPS server, fill empty (default) for HTTP server
- `-genkey` - set with non-empty `-crt` and `-key` for generate new TLS certificate, will override the file set with `-crt <crt.pem>` and `-key <key.pem>`


## Important to know

- must be **login to edit any thing**, otherwise data won't save
- TiddlyWeb plugin must be config correctly:
  - default base TW5 HTML file already done for you.
  - default value of TiddlyWeb in `$:/plugins/tiddlywiki/tiddlyweb/save/offline` only save a static HTML file, will become non-editable after fist save & reload, must be edit for save a working TW5 base HTML file. (see below `Make a TiddlyWiki base image`)
- following notes assumes that TiddlyWeb plugin had beeen config correctly
  - when click "Save Button", tiddlers and drafts won't be embedded into base HTML file, only plugins and configs will be embedded.
  - tiddlers and drafts are save/modify via TiddlyWeb, they won't be embedded into base HTML file.
  - install plugins **MUST click 'Save Button' manually** to cause a full upload of base HTML file, then reload the page (F5 should be fine) to activate.
- about "Export all": all **tiddlers MUST be loaded** and then do a export, otherwise the tiddlers which did not loaded will only have title!!


## Different between PutSaver, TiddlyWeb and both enable

|                                      | PutSaver only [1]            | TiddlyWeb only                                                        | TiddlyWeb and PutSaver [2]  |
|--------------------------------------|------------------------------|-----------------------------------------------------------------------|-----------------------------|
| can install plugin                   | yes [3]                      | no, need update base file                                             | yes [4], click 'Save'       |
| update sending size                  | big, full html file (~2MB)   | little (~ tiddler's size)                                             | little, except click 'Save' |
| load tiddlers/configs from base file | once when page opened        | same as 'Save only'                                                   | same as 'Save only'         |
| load tiddlers/configs by ajax        | no                           | yes, can override base file values [5]                                | same as 'TiddlyWeb only'    |
| save tiddlers/configs into base file | yes [3][4]                   | no                                                                    | yes [4], click 'Save'       |
| save tiddlers/configs by ajax        | no                           | yes                                                                   | yes                         |
| loading timing                       | all in once when page opened | data in base file when page opened and then load others with ajax     | same as 'TiddlyWeb only'    |
| Export all tiddlers                  | yes, at any time             | yes, but tiddlers must be loaded, otherwise will only have title      | same as 'TiddlyWeb only'    |


- [1] base on WebDAV
- [2] this implement, TiddlyWeb plugin must be config correctly
- [3] need to disable all authorization in current implement (modify code), or use other WebDAV server
- [4] by using PutSaver (WebDAV), need login, cause a full upload of base file
- [5] `$:/StoryList` not work :(


## Make a TiddlyWiki base image

The TiddlyWiki code is stored in and served from index.html, which
(as you can see by clicking on the Tools tab) is TiddlyWiki version 5.1.17.

Plugins must be pre-baked into the TiddlyWiki file, not stored on the server
as lazily loaded Tiddlers. The index.html in this directory is 5.1.17 with
the TiddlyWeb added. The TiddlyWeb plugin is required, so that index.html talks back to the server for content.

The process for preparing a new index.html is:

- Open tiddlywiki-5.1.17.html in your web browser.
- Click the control panel (gear) icon.
- Click the Plugins tab.
- Click "Get more plugins".
- Click "Open plugin library".
- Type "tiddlyweb" into the search box. The "TiddlyWeb and TiddlySpace components" should appear.
- Click Install. A bar at the top of the page should say "Please save and reload for the changes to take effect."
- edit `$:/plugins/tiddlywiki/tiddlyweb/save/offline` (need some time for loading & saving)
  - not save openlist: `[all[]] -[[$:/HistoryList]] -[[$:/StoryList]] -[[$:/Import]] -[[$:/isEncrypted]] -[[$:/UploadName]] -[prefix[$:/state/]] -[prefix[$:/temp/]] -[field:bag[bag]] -[has[draft.of]]`
  - save openlist: `[all[]] -[[$:/HistoryList]] -[[$:/Import]] -[[$:/isEncrypted]] -[[$:/UploadName]] -[prefix[$:/state/]] -[prefix[$:/temp/]] -[field:bag[bag]] -[has[draft.of]]`
- Click the icon next to save, and an updated file will be downloaded.
- Open the downloaded file in the web browser.
- Repeat, adding any more plugins. Or add more later when "widdly" start.
- Copy the final download to index.html.

## Similar projects

For a Google App Engine TiddlyWiki server, look at [rsc/tiddly](https://github.com/rsc/tiddly).


## SQLite backend
There are some tweaking option for the trade off between disk IO and data safety, edit `Open()` function in `store/sqlite/sqlite.go` for your use case and re-compile the code.
Default option are `journal_mode = WAL` and `synchronous = NORMAL`.


## TODO

- [ ] `$:/DefaultTiddlers` loaded but not show up, might be cause by `$:/StoryList`
  - [x] ignore PUT `$:/StoryList` to prevent multi-tabs/users "Open List" conflict
- [x] add authorization back
- [ ] multiple TiddlyWiki in subpath/suburl
- [ ] ACL: login for read & edit, login for edit, all can edit
- [x] check user/pass in file/db
- [ ] fix api_test.go & add more test
- [x] set max keeping history revisions
  - [x] flat file
  - [x] bolt/bbolt
  - [x] sqlite
- [ ] reduce history revisions size
  - [ ] flat file
  - [ ] bolt/bbolt
  - [ ] sqlite
- [x] send base html with gzip
- [x] select backend type without re-compile
- [ ] https server
  - [x] generate TLS certificate
  - [x] serve in https
  - [ ] auto let's encrypt certificate
- [ ] backend
  - [ ] json file backend
    - [ ] full sync
    - [ ] flush on exit & sync by time
  - [ ] dump & import data
  - [ ] convert between different backend type

