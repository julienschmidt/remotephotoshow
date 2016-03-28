# remotephotoshow
A small server application to present photos remotely over the web

The whole family is gathered for christmas. The whole family?
Nooo, Julien is more than 10000 km away.

This is a small web app I made to show my family some photos remotely over the web using [Server-Sent Events](http://www.w3.org/TR/eventsource/) in Go (using [the sse package](https://github.com/julienschmidt/sse)).

## Usage
Modify the the [config](https://github.com/julienschmidt/remotephotoshow/blob/master/server.go#L25), put your photos in the configured directory and you are ready to run the app with `go run server.go`!

Protip™: You can use your arrow keys in the master mode!


A small hack by [@julienschmidt](https://twitter.com/JulienSchmidt)
