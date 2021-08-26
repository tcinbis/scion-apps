module github.com/netsec-ethz/scion-apps

go 1.16

require (
	github.com/HdrHistogram/hdrhistogram-go v1.1.0 // indirect
	github.com/bclicn/color v0.0.0-20180711051946-108f2023dc84
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/gorilla/handlers v1.5.1
	github.com/inconshreveable/log15 v0.0.0-20180818164646-67afb5ed74ec
	github.com/kormat/fmt15 v0.0.0-20181112140556-ee69fecb2656
	github.com/kr/pty v1.1.8
	github.com/lucas-clemente/quic-go v0.21.1
	github.com/mattn/go-sqlite3 v1.14.4
	github.com/msteinert/pam v0.0.0-20190215180659-f29b9f28d6f9
	github.com/netsec-ethz/rains v0.2.0
	github.com/pelletier/go-toml v1.9.3
	github.com/scionproto/scion v0.6.0
	github.com/smartystreets/goconvey v1.6.4
	github.com/uber/jaeger-lib v2.4.1+incompatible // indirect
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/image v0.0.0-20191009234506-e7c1f5e7dbb8
	golang.org/x/net v0.0.0-20210505024714-0287a6fb4125 // indirect
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
)

//replace github.com/scionproto/scion => github.com/netsec-ethz/scion v0.0.0-20210705084436-3295af71a57a
//
//replace github.com/lucas-clemente/quic-go => github.com/tcinbis/quic-go v0.21.0-flowtele-rc-1
replace github.com/scionproto/scion => ./scion

replace github.com/lucas-clemente/quic-go => ./quic-go
