# HowTo

First we have to download the data which is going to be streamed as well as the certificates the server is using.
You can find an archive [here][1]. For the password ask the creator of the archive.

```
# Decryption and unarchive
openssl enc -aes-256-cbc -d -in flowtelestreamserver.tar.gz.enc | tar xz
```

Unpack the archive to `~/go/src/scion-apps/_examples/flowtele` such that you find two new directories `data` and
`letsencrypt` next to the `server.go` file.

Now start the server as follows.
```[bash]
go run . --port 8080 \
--certs /home/your-username/go/src/scion-apps/_examples/flowtele/letsencrypt \
--data /home/your-username/go/src/scion-apps/_examples/flowtele
```

[1]: https://google.com