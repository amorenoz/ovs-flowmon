#!/bin/bash
set -eu

SCRIPT=`realpath -s $0`
SCRIPT_PATH=`dirname $SCRIPT`

TEMPLATE=${SCRIPT_PATH}/dist/ovs-flowmon.yaml.j2
POD_SPEC=${SCRIPT_PATH}/build/ovs-flowmon.yaml
IMAGE=quay.io/amorenoz/ovs-flowmon

function wait_for {
  # Execute in a subshell to prevent local variable override during recursion
  (
    local total_attempts=$1; shift
    local cmdstr=$*
    local sleep_time=2
    echo -e "\n[wait_for] Waiting for cmd to return success: ${cmdstr}"
    # shellcheck disable=SC2034
    for attempt in $(seq "${total_attempts}"); do
      echo "[wait_for] Attempt ${attempt}/${total_attempts%.*} for: ${cmdstr}"
      # shellcheck disable=SC2015
      eval "${cmdstr}" && echo "[wait_for] OK: ${cmdstr}" && return 0 || true
      sleep "${sleep_time}"
    done
    echo "[wait_for] ERROR: Failed after max attempts: ${cmdstr}"
    return 1
  )
}

usage() {
    echo "$0 [OPTIONS] NODE_NAME "
    echo ""
    echo "Deploy the ovs-flowmon to debug the OVS running on a k8s NODE"
    echo ""
    echo "Options"
    echo "  -i IMAGE   Use a different container image"
    echo ""
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

# Ensure j2 installed
pip freeze | grep j2cli || pip install j2cli[yaml] --user

kubectl get node $NODE &>/dev/null || "kubectl cannot access node $NODE. Ensure the node name is correct and you have access to the cluster (KUBECONFIG)"

mkdir -p ${SCRIPT_PATH}/build
node=${NODE} \
    image=${IMAGE} \
    j2 ${TEMPLATE} -o ${POD_SPEC}

kubectl label nodes --overwrite ${NODE} flowmon=true

kubectl apply -f ${POD_SPEC}

wait_for 20 'test $(kubectl get pods | grep -e "ovs-flowmon" | grep "Running" -c ) -eq 1'

kubectl exec -it ovs-flowmon-${NODE} /root/run



