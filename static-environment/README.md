# How to the static network environment

Because we want to create two virtual bottlenecks we will first need two virtual network interfaces which we can shape with `tc`.

## Configure host interfaces

You can run `virtual-network.sh` to automatically create two interfaces called `macvlan1` and `macvlan2` with IP space `10.10.1.0/24` and `10.10.2.0/24` respectively.
The interface on the host machine will always be assigned the `10.10.x.1` address.
If you want to delete the interfaces and set them up again you can add the `-d` flag to signify that the interfaces need to be delted first.
Check the script for how the interfaces are shaped respectively.

## Configure your VMs

Once these interfaces exist and they are shaped they need to be assigned two the virtual machines running SCIONLab.
If you are using Vagrant you can add one or both of these lines to the `Vagrantfile` to configure extra bridged network interfaces for your VM.

```
config.vm.network "public_network", ip: "10.10.1.2", bridge: "macvlan1"
config.vm.network "public_network", ip: "10.10.2.2", bridge: "macvlan2"
```

In this case the machine will be assigned two additional interface with the IPs `10.10.1.2` and `10.10.2.2` bridged over `macvlan1` and `macvlan2` on the host machine.

## Update SCIONLab topology

Finally, we have to edit the topology files of the VMs located under `/etc/scion/topology.json`.
There we first replace every occurence of `127.0.0.1:xxxx` with one of the IPs of the new bridged interfaces.

If you want to connect two VMs via a direct local link check out the included topology examples `17-ffaa-1-ec7.topo.json` and `17-ffaa-1-f1b.topo.json`.