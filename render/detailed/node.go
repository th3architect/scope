package detailed

import (
	"sort"

	"github.com/weaveworks/scope/probe/docker"
	"github.com/weaveworks/scope/probe/host"
	"github.com/weaveworks/scope/probe/kubernetes"
	"github.com/weaveworks/scope/probe/process"
	"github.com/weaveworks/scope/render"
	"github.com/weaveworks/scope/report"
)

// Node is the data type that's yielded to the JavaScript layer when
// we want deep information about an individual node.
type Node struct {
	NodeSummary
	Rank     string             `json:"rank,omitempty"`
	Pseudo   bool               `json:"pseudo,omitempty"`
	Controls []ControlInstance  `json:"controls"`
	Children []NodeSummaryGroup `json:"children,omitempty"`
	Parents  []Parent           `json:"parents,omitempty"`
}

// Parent is the information needed to build a link to the parent of a Node.
type Parent struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	TopologyID string `json:"topologyId"`
}

// ControlInstance contains a control description, and all the info
// needed to execute it.
type ControlInstance struct {
	ProbeID string `json:"probeId"`
	NodeID  string `json:"nodeId"`
	report.Control
}

// MakeNode transforms a renderable node to a detailed node. It uses
// aggregate metadata, plus the set of origin node IDs, to produce tables.
func MakeNode(r report.Report, n render.RenderableNode) Node {
	summary, _ := MakeNodeSummary(n.Node)
	summary.ID = n.ID
	summary.Label = n.LabelMajor
	return Node{
		NodeSummary: summary,
		Rank:        n.Rank,
		Pseudo:      n.Pseudo,
		Controls:    controls(r, n),
		Children:    children(n),
		Parents:     parents(r, n),
	}
}

func controlsFor(topology report.Topology, nodeID string) []ControlInstance {
	result := []ControlInstance{}
	node, ok := topology.Nodes[nodeID]
	if !ok {
		return result
	}

	for _, id := range node.Controls.Controls {
		if control, ok := topology.Controls[id]; ok {
			probeID, _ := node.Latest.Lookup(report.ProbeID)
			result = append(result, ControlInstance{
				ProbeID: probeID,
				NodeID:  nodeID,
				Control: control,
			})
		}
	}
	return result
}

func controls(r report.Report, n render.RenderableNode) []ControlInstance {
	if _, ok := r.Process.Nodes[n.ControlNode]; ok {
		return controlsFor(r.Process, n.ControlNode)
	} else if _, ok := r.Container.Nodes[n.ControlNode]; ok {
		return controlsFor(r.Container, n.ControlNode)
	} else if _, ok := r.ContainerImage.Nodes[n.ControlNode]; ok {
		return controlsFor(r.ContainerImage, n.ControlNode)
	} else if _, ok := r.Host.Nodes[n.ControlNode]; ok {
		return controlsFor(r.Host, n.ControlNode)
	}
	return []ControlInstance{}
}

var (
	nodeSummaryGroupSpecs = []struct {
		topologyID string
		NodeSummaryGroup
	}{
		{report.Host, NodeSummaryGroup{TopologyID: "hosts", Label: "Hosts", Columns: []string{host.CPUUsage, host.MemoryUsage}}},
		{report.Pod, NodeSummaryGroup{TopologyID: "pods", Label: "Pods", Columns: []string{}}},
		{report.ContainerImage, NodeSummaryGroup{TopologyID: "containers-by-image", Label: "Container Images", Columns: []string{}}},
		{report.Container, NodeSummaryGroup{TopologyID: "containers", Label: "Containers", Columns: []string{docker.CPUTotalUsage, docker.MemoryUsage}}},
		{report.Process, NodeSummaryGroup{TopologyID: "applications", Label: "Applications", Columns: []string{process.PID, process.CPUUsage, process.MemoryUsage}}},
	}
)

func children(n render.RenderableNode) []NodeSummaryGroup {
	summaries := map[string][]NodeSummary{}
	for _, child := range n.Children {
		if child.ID == n.ID {
			continue
		}

		if summary, ok := MakeNodeSummary(child); ok {
			summaries[child.Topology] = append(summaries[child.Topology], summary)
		}
	}

	nodeSummaryGroups := []NodeSummaryGroup{}
	for _, spec := range nodeSummaryGroupSpecs {
		if len(summaries[spec.topologyID]) > 0 {
			sort.Sort(nodeSummariesByID(summaries[spec.TopologyID]))
			group := spec.NodeSummaryGroup.Copy()
			group.Nodes = summaries[spec.topologyID]
			nodeSummaryGroups = append(nodeSummaryGroups, group)
		}
	}
	return nodeSummaryGroups
}

// parents renders the parents of this report.Node, which have been aggregated
// from the probe reports.
func parents(r report.Report, n render.RenderableNode) (result []Parent) {
	topologies := map[string]struct {
		report.Topology
		render func(report.Node) Parent
	}{
		report.Container:      {r.Container, containerParent},
		report.Pod:            {r.Pod, podParent},
		report.Service:        {r.Service, serviceParent},
		report.ContainerImage: {r.ContainerImage, containerImageParent},
		report.Host:           {r.Host, hostParent},
	}
	topologyIDs := []string{}
	for topologyID := range topologies {
		topologyIDs = append(topologyIDs, topologyID)
	}
	sort.Strings(topologyIDs)
	for _, topologyID := range topologyIDs {
		t := topologies[topologyID]
		parents, _ := n.Node.Parents.Lookup(topologyID)
		for _, id := range parents {
			if topologyID == n.Node.Topology && id == n.ID {
				continue
			}

			parent, ok := t.Nodes[id]
			if !ok {
				continue
			}

			result = append(result, t.render(parent))
		}
	}
	return result
}

func containerParent(n report.Node) Parent {
	label, _ := render.GetRenderableContainerName(n)
	containerID, _ := n.Latest.Lookup(docker.ContainerID)
	return Parent{
		ID:         render.MakeContainerID(containerID),
		Label:      label,
		TopologyID: "containers",
	}
}

func podParent(n report.Node) Parent {
	podID, _ := n.Latest.Lookup(kubernetes.PodID)
	podName, _ := n.Latest.Lookup(kubernetes.PodName)
	return Parent{
		ID:         render.MakePodID(podID),
		Label:      podName,
		TopologyID: "pods",
	}
}

func serviceParent(n report.Node) Parent {
	serviceID, _ := n.Latest.Lookup(kubernetes.ServiceID)
	serviceName, _ := n.Latest.Lookup(kubernetes.ServiceName)
	return Parent{
		ID:         render.MakeServiceID(serviceID),
		Label:      serviceName,
		TopologyID: "pods-by-service",
	}
}

func containerImageParent(n report.Node) Parent {
	imageName, _ := n.Latest.Lookup(docker.ImageName)
	return Parent{
		ID:         render.MakeContainerImageID(render.ImageNameWithoutVersion(imageName)),
		Label:      imageName,
		TopologyID: "containers-by-image",
	}
}

func hostParent(n report.Node) Parent {
	hostName, _ := n.Latest.Lookup(host.HostName)
	return Parent{
		ID:         render.MakeHostID(hostName),
		Label:      hostName,
		TopologyID: "hosts",
	}
}
