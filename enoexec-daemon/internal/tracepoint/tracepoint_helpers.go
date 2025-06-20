package tracepoint

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/openshift/multiarch-tuning-operator/enoexec-daemon/internal/utils"
)

// getPodContainerUUIDFor retrieves the pod UUID and container ID for a given process ID (pid).
// It reads the cgroup file for the process and extracts the pod UUID and container ID from it.
// The container ID is returned in the format "cri-o://<container_id>".
// The pod UUID is extracted from the cgroup path and returned as a string.
func getPodContainerUUIDFor(pid int32) (string, string, error) {
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	file, err := os.Open(cgroupPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to open %s: %w", cgroupPath, err)
	}
	defer utils.Should(file.Close())
	scanner := bufio.NewScanner(file)
	var out string
	for scanner.Scan() {
		out += scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("error reading %s: %w", cgroupPath, err)
	}
	// The output `out` looks like this:
	// /kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod15778196_6e1f_4b5d_8012_12c7bf5c04b3.slice/crio-4015cec33e493690b48385efa86af20938e5b80077094cbc5e875945178d57be.scope

	// Extract the container ID from the cgroup path
	outSplit := strings.Split(out, "/")
	containerID := outSplit[len(outSplit)-1]
	// containerID looks like: crio-4015cec33e493690b48385efa86af20938e5b80077094cbc5e875945178d57be.scope
	containerID = strings.TrimSuffix(containerID, ".scope")
	containerID = strings.Replace(containerID, "crio-", "cri-o://", 1)
	// containerID is now in the format: cri-o://4015cec33e493690b48385efa86af20938e5b80077094cbc5e875945178d57be

	// Extract the pod UUID from the cgroup path
	re := regexp.MustCompile(`pod([0-9a-fA-F_]{36})\.slice`)
	match := re.FindStringSubmatch(out)
	// match[0] is the full match, match[1] is the pod UUID
	podUID := ""
	if len(match) >= 2 {
		podUID = strings.ReplaceAll(match[1], "_", "-")
	}

	return podUID, containerID, nil
}

// getPodNameFromUUID retrieves the pod name and namespace from the CRI-O runtime
// using the pod UUID. It connects to the CRI-O socket and lists all pod sandboxes,
// searching for the pod with the given UUID. If found, it returns the pod name and namespace.
// If the pod is not found, it returns an empty string for both name and namespace.
// It returns an error if the connection to the CRI-O socket fails or if other critical operations fail.
// If the pod is not found, it returns empty strings for both name and namespace without an error.
func getPodNameFromUUID(ctx context.Context, uid string) (string, string, error) {
	// Path to CRI-O socket
	// TODO: Other CRI runtimes
	cri, err := grpc.NewClient("unix:///var/run/crio/crio.sock",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", "", fmt.Errorf("failed to connect to CRI socket: %w", err)
	}
	defer utils.Should(cri.Close())
	criClient := runtimeapi.NewRuntimeServiceClient(cri)
	pods, err := criClient.ListPodSandbox(ctx, &runtimeapi.ListPodSandboxRequest{})
	if err != nil {
		return "", "", fmt.Errorf("failed to list pod sandboxes: %w", err)
	}
	if pods == nil || len(pods.Items) == 0 {
		fmt.Println("No pods found")
		return "", "", nil
	}
	for _, pod := range pods.Items {
		if pod.Metadata.GetUid() == uid {
			return pod.Metadata.Name, pod.Metadata.Namespace, nil
		}
	}
	return "", "", nil
}
