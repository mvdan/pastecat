## Pastecat

A very simple and self-hosted pastebin service written in Go. Stores
plaintext pastes in a filesystem hierarchy.

Designed to remove pastes after a certain period of time. Upon restart, file
modification times will be used to recover the removal time of each paste.

This software is what runs [paste.cat](http://paste.cat) for public use.

#### Build

	$ go build pastecat.go

#### Run

Quick setup:

	$ pastecat -u http://my.site -l :80 -d /tmp/paste

Options:

* **-u** - URL of the site - *http://localhost:8080*
* **-l** - Host and port to listen to - *:8080*
* **-d** - Directory to store all the pastes in - *data*
* **-t** - Lifetime of the pastes - *12h*
* **-s** - Maximum size of pastes - *1M*
* **-m** - Maximum number of pastes to store at once - *0*
* **-M** - Maximum storage size to use at once - *1G*
* **-T** - Timeout of requests - *200ms*

It will stay in the foreground and periodically print usage stats.

#### Use

Set up an alias for easy usage:

	$ alias pcat='curl -F "paste=<-" http://paste.cat'

Upload a new paste via standard input:

	$ echo foo | pcat
	http://paste.cat/a63d03b9

Fetch it:

	$ curl ̣http://paste.cat/a63d03b9
	foo

#### What it doesn't do

##### Compression

Should be handled at a lower level. Filesystems like Btrfs already support
compression.

##### HTTPS

All pastes are public even if urls are not easily found. You can use tools
like GnuPG if you wish to sign or encrypt your data and then upload it in a
format like ASCII armored.

The only real reason why HTTPS might be needed is to fetch sensitive data that
someone else uploaded without signing nor encrypting it. In which case the
solution would be to not upload it in bare plain text to begin with.
