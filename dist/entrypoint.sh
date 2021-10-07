#!/bin/bash
set -ex

OVS_TARGET=${OVS_TARGET:-unix:/var/run/openvswitch/db.sock}


cat > /root/run <<EOF 
#!/bin/bash

exec ovs-flowmon -ovsdb ${OVS_TARGET}

EOF

chmod +x /root/run

exec sleep infinity 

