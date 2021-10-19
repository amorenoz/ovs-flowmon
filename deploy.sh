#!/bin/bash
set -eu

SCRIPT=`realpath -s $0`
SCRIPT_PATH=`dirname $SCRIPT`

TEMPLATE=${SCRIPT_PATH}/dist/ovs-flowmon.yaml.j2
POD_SPEC=${SCRIPT_PATH}/build/ovs-flowmon.yaml
IMAGE=quay.io/amorenoz/ovs-flowmon

usage() {
    echo "$0 [OPTIONS] NODE_NAME "
    echo ""
    echo "Deploy the ovs-flowmon to debug the OVS running on a k8s NODE"
    echo ""
    echo "Options"
    echo "  -i IMAGE   Use a different container image"
    echo ""
}

is_command_fail() {
	set +e
	local cmd=$1
	if ! command -v $cmd &> /dev/null; then
		echo "ERROR - command '$cmd' not found, exiting."
		exit 1
	fi
	set -e
}

KUBECTL=""
get_kubectl_binary() {
	set +e
	KUBECTL=$(which oc 2>/dev/null)
	ret_code="$?"
	if [ $ret_code -eq 0 ]; then
		set -e
		return
	fi

	# Note: "local OC_LOCATION=" will set the return code to 0, always
	# so must keep this in public scope
	KUBECTL=$(which kubectl 2>/dev/null)
	ret_code="$?"
	if [ $ret_code -eq 0 ]; then
		set -e
		return
	fi

	echo "ERROR - could not find oc/kubectl binary, exiting."
	exit 1
}

if [ $# -lt 1 ]; then
    usage
    exit 1
fi

while getopts ":hi:" opt; do
    case ${opt} in
        h)
            usage
            exit 0
            ;;
        i)
            IMAGE=$OPTARG
            ;;
    esac
done

shift $(((OPTIND -1)))
NODE=$1

get_kubectl_binary
is_command_fail pip
is_command_fail grep

# Ensure j2 installed
pip freeze | grep j2cli || pip install j2cli[yaml] --user

# j2 may be installed, but the pip user path might not be added to PATH
is_command_fail j2

$KUBECTL get node $NODE &>/dev/null || "kubectl cannot access node $NODE. Ensure the node name is correct and you have access to the cluster (KUBECONFIG)"

mkdir -p ${SCRIPT_PATH}/build
node=${NODE} \
    image=${IMAGE} \
    j2 ${TEMPLATE} -o ${POD_SPEC}

$KUBECTL label nodes --overwrite ${NODE} flowmon=true

$KUBECTL apply -f ${POD_SPEC}

echo "Waiting for pod to switch to ready condition. This can take a while (timeout 300s) ..."
$KUBECTL wait pod --for=condition=ready --timeout=300s -l app=ovs-flowmon

$KUBECTL exec -it ovs-flowmon-${NODE} -- /root/run
