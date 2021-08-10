#!/bin/bash
set -e # exit on error

VIRT1="macvlan1"
VIRT1_IP="10.10.1.1"
VIRT2="macvlan2"
VIRT2_IP="10.10.2.1"

red=$(tput setaf 1)
green=$(tput setaf 2)
yellow=$(tput setaf 3)
reset=$(tput sgr0)

function print_success_message() {
  printf -- "${green}$1${reset}\n" >&1;
}

function print_error_message() {
  printf -- "${red}$1${reset}\n" >&2;
}

function print_progress_message() {
  printf -- "${yellow}$1${reset}\n" >&1;
}

function print_help() {
  cat << EOF
Usage: $0 [OPTIONS]

[OPTIONS]
  -h, --help  display this help and exit
  -d          delete interfaces before adding them
EOF
}

function delete_interfaces() {
  ip link delete $VIRT1 || true
  ip link delete $VIRT2 || true
}

function add_interfaces() {
  echo "Select a network interface to bridge on:"
  NETWORK_INTERFACE=${NETWORK_INTERFACE:-}
  cd /sys/class/net && select NETWORK_INTERFACE in *; do echo Selected: $NETWORK_INTERFACE; break; done; cd -

  print_progress_message "Adding ${VIRT1} with IP ${VIRT1_IP}"
  ip link add $VIRT1 link $NETWORK_INTERFACE type macvlan mode bridge
  ifconfig $VIRT1 $VIRT1_IP up netmask 255.255.255.0

  print_progress_message "Adding ${VIRT2} with IP ${VIRT2_IP}"
  ip link add $VIRT2 link $NETWORK_INTERFACE type macvlan mode bridge
  ifconfig $VIRT2 $VIRT2_IP up netmask 255.255.255.0

  print_progress_message "Configuring network interfaces complete."
}

if [ ! "${LOGNAME}" == "root" ]; then
  cat << EOF
${red}ERROR: You are trying to run this script with the user ${USER} instead of root.
privileged access is required to setup the network interfaces.${reset}
EOF
  exit 1
fi

if [ ${#@} -ne 0 ]; then
    if [[ "${@#"--help"}" = "" ]] || [[ "${@#"-h"}" = "" ]]; then
      print_help;
      exit 0;
    fi
  fi

DELETE_INTERFACES=false
while getopts ":d" "flag"; do
    case "${flag}" in
        d) DELETE_INTERFACES=true;;
        a) age=${OPTARG};;
        f) fullname=${OPTARG};;
    esac
done

if [[ "$DELETE_INTERFACES" == "true" ]]; then
    print_progress_message "Deleting network interfaces ${VIRT1} and ${VIRT2}..."
    delete_interfaces
fi
add_interfaces

print_progress_message "Setting up tc..."

tc qdisc add dev $VIRT1 root tbf rate 5Mbit burst 25kb latency 100ms
tc qdisc add dev $VIRT2 root tbf rate 10Mbit burst 50kb latency 150ms

print_progress_message "tc configuredto:"
tc qdisc list dev $VIRT1
tc qdisc list dev $VIRT2

print_success_message "Setup complete."