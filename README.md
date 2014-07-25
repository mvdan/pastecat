## Paste.cat

A very simple and self-hosted pastebin service written in Go. Stores
zlib-compressed pastes in a filesystem hierarchy.

Designed to remove pastes after a certain period of time. Upon restart, file
modify times will be used to try to recover the removal time of each paste.

#### Build

	$ go build pastecat.go

#### Run

Default options:

	$ ./pastecat

Custom options:

	$ ./pastecat -u http://my.site -l hostname:80 -d /tmp/paste -t 1h -s 2M

It will stay in the foreground and log paste activity and errors.

#### Use

Upload a new paste:

	$ echo foo | curl -F 'paste=<-' http://paste.cat
	http://paste.cat/a63n03rp

Fetch it:

	$ curl http://paste.cat/a63n03rp
	foo

Alternatively, you can use the help of a shell function:

	pcat() {
		if [ -t 0 ]; then
			[ $# -gt 0 ] || return 1
			cat "$*" | curl -F 'paste=<-' http://paste.cat
		else
			curl -F 'paste=<-' http://paste.cat
		fi
	}

This will allow for easier usage:

	$ pcat file
	$ echo foo | pcat
