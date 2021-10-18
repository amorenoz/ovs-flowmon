# ovn-flowmon
Terminal-based NetFlow/sFlow/IPFIX flow monitoring for OpenvSwitch

Implemented using [tview](https://github.com/rivo/tview) (for graphics) and [goflow2](https://github.com/netsampler/goflow2) (for IPFIX collection).


## Building

    make

## Usage

### Locally
Start the ovn-flowmon daemon

    ./build/ovs-flowmon


### Kubernetes

Use the deploy script to deploy the monitor in the node you want to monitor:

    ./deploy.sh [ -i NON_DEFAULT_IMAGE ] NODE_NAME


To clean the deployment simply delete the ovs-flowmon pod:

    kubectl delete pod -l app=ovs-flowmon


## Demo
[![asciicast](https://asciinema.org/a/440615.svg)](https://asciinema.org/a/440615)
