# ovn-flowmon
Terminal-based NetFlow/sFlow/IPFIX flow monitoring for OpenvSwitch

## Building

    make

## Usage
Start the ovn-flowmon daemon

    ./build/ovn-flowmon

(The tui will ask you to press 'Enter' on the 'Start' button)

Start ovs IPFIX reporter

    ovs-vsctl -- set Bridge br-int ipfix=@i -- --id=@i create IPFIX targets=\"OVS_FLOWMON_IP:2055\"

Where `OVS_FLOWMON_IP` is any IP address ovn-flowmon is listening to (by default, any IP from that host)

