// Copyright 2020 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// example-shttp-fileserver is a simple HTTP fileserver that serves all files
// and subdirectories under the current working directory.
package main

import (
	"fmt"
	"github.com/gorilla/handlers"
	"github.com/netsec-ethz/scion-apps/pkg/shttp"
	"log"
	"net/http"
	"os"
)

func main() {
	port := 8081
	dir := "/home/tom/go/src/scion-apps/_examples/flowtele"

	handler := handlers.LoggingHandler(
		os.Stdout,
		http.FileServer(http.Dir(dir)),
	)

	//profile.Start(profile.BlockProfile, profile.ProfilePath("."))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	})

	log.Fatal(shttp.ListenAndServe(fmt.Sprintf(":%d", port), mux, nil, nil))
}
