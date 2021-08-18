#!/usr/bin/env zsh

docker build -t scionapps-build --build-arg USER_ID="$(id -u)" --build-arg GROUP_ID="$(id -g)" .
docker run -it --rm -v "$(pwd)":/home/scion/scion-apps scionapps-build /bin/bash -c "cd scion-apps && make flowtele && exit"

cp bin/example-flowtele-listener ~/git-repos/scion-flowtele/ansible/roles/flowtele-listener/files/flowtele_listener
cp bin/example-flowtele-socket ~/git-repos/scion-flowtele/ansible/roles/flowtele-socket/files/flowtele_socket