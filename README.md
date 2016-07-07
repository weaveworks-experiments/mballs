# mweb
mweb is a program to demo multicast.

Run it multiple times on different machines/containers and each
instance will learn about the others through multicast.

Hit it via http on port 8080 and it will return a list of instances.

Flags:
 - `--iface` makes it use (and wait for) a particular interface (e.g. `ethwe`)
 - `-p` makes it listen on a different http port
