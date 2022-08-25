package cloudcmd

import (
	"context"
	"fmt"
	"io"

	azurecl "github.com/edgelesssys/constellation/cli/internal/azure/client"
	"github.com/edgelesssys/constellation/cli/internal/gcp"
	gcpcl "github.com/edgelesssys/constellation/cli/internal/gcp/client"
	"github.com/edgelesssys/constellation/internal/cloud/cloudprovider"
	"github.com/edgelesssys/constellation/internal/cloud/cloudtypes"
	"github.com/edgelesssys/constellation/internal/config"
	"github.com/edgelesssys/constellation/internal/state"
)

// Creator creates cloud resources.
type Creator struct {
	out            io.Writer
	newGCPClient   func(ctx context.Context, project, zone, region, name string) (gcpclient, error)
	newAzureClient func(subscriptionID, tenantID, name, location string) (azureclient, error)
}

// NewCreator creates a new creator.
func NewCreator(out io.Writer) *Creator {
	return &Creator{
		out: out,
		newGCPClient: func(ctx context.Context, project, zone, region, name string) (gcpclient, error) {
			return gcpcl.NewInitialized(ctx, project, zone, region, name)
		},
		newAzureClient: func(subscriptionID, tenantID, name, location string) (azureclient, error) {
			return azurecl.NewInitialized(subscriptionID, tenantID, name, location)
		},
	}
}

// Create creates the handed amount of instances and all the needed resources.
func (c *Creator) Create(ctx context.Context, provider cloudprovider.Provider, config *config.Config, name, insType string, controlPlaneCount, workerCount int,
) (state.ConstellationState, error) {
	switch provider {
	case cloudprovider.GCP:
		cl, err := c.newGCPClient(
			ctx,
			config.Provider.GCP.Project,
			config.Provider.GCP.Zone,
			config.Provider.GCP.Region,
			name,
		)
		if err != nil {
			return state.ConstellationState{}, err
		}
		defer cl.Close()
		return c.createGCP(ctx, cl, config, insType, controlPlaneCount, workerCount)
	case cloudprovider.Azure:
		cl, err := c.newAzureClient(
			config.Provider.Azure.SubscriptionID,
			config.Provider.Azure.TenantID,
			name,
			config.Provider.Azure.Location,
		)
		if err != nil {
			return state.ConstellationState{}, err
		}
		return c.createAzure(ctx, cl, config, insType, controlPlaneCount, workerCount)
	default:
		return state.ConstellationState{}, fmt.Errorf("unsupported cloud provider: %s", provider)
	}
}

func (c *Creator) createGCP(ctx context.Context, cl gcpclient, config *config.Config, insType string, controlPlaneCount, workerCount int,
) (stat state.ConstellationState, retErr error) {
	defer rollbackOnError(context.Background(), c.out, &retErr, &rollbackerGCP{client: cl})

	if err := cl.CreateVPCs(ctx); err != nil {
		return state.ConstellationState{}, err
	}
	if err := cl.CreateFirewall(ctx, gcpcl.FirewallInput{
		Ingress: cloudtypes.Firewall(config.IngressFirewall),
		Egress:  cloudtypes.Firewall(config.EgressFirewall),
	}); err != nil {
		return state.ConstellationState{}, err
	}

	// additionally create allow-internal rules
	internalFirewallInput := gcpcl.FirewallInput{
		Ingress: cloudtypes.Firewall{
			{
				Name:     "allow-cluster-internal-tcp",
				Protocol: "tcp",
				IPRange:  gcpcl.SubnetExtCIDR,
			},
			{
				Name:     "allow-cluster-internal-udp",
				Protocol: "udp",
				IPRange:  gcpcl.SubnetExtCIDR,
			},
			{
				Name:     "allow-cluster-internal-icmp",
				Protocol: "icmp",
				IPRange:  gcpcl.SubnetExtCIDR,
			},
			{
				Name:     "allow-node-internal-tcp",
				Protocol: "tcp",
				IPRange:  gcpcl.SubnetCIDR,
			},
			{
				Name:     "allow-node-internal-udp",
				Protocol: "udp",
				IPRange:  gcpcl.SubnetCIDR,
			},
			{
				Name:     "allow-node-internal-icmp",
				Protocol: "icmp",
				IPRange:  gcpcl.SubnetCIDR,
			},
		},
	}
	if err := cl.CreateFirewall(ctx, internalFirewallInput); err != nil {
		return state.ConstellationState{}, err
	}

	createInput := gcpcl.CreateInstancesInput{
		CountControlPlanes: controlPlaneCount,
		CountWorkers:       workerCount,
		ImageID:            config.Provider.GCP.Image,
		InstanceType:       insType,
		StateDiskSizeGB:    config.StateDiskSizeGB,
		StateDiskType:      config.Provider.GCP.StateDiskType,
		KubeEnv:            gcp.KubeEnv,
	}
	if err := cl.CreateInstances(ctx, createInput); err != nil {
		return state.ConstellationState{}, err
	}

	if err := cl.CreateLoadBalancers(ctx); err != nil {
		return state.ConstellationState{}, err
	}

	return cl.GetState(), nil
}

func (c *Creator) createAzure(ctx context.Context, cl azureclient, config *config.Config, insType string, controlPlaneCount, workerCount int,
) (stat state.ConstellationState, retErr error) {
	defer rollbackOnError(context.Background(), c.out, &retErr, &rollbackerAzure{client: cl})

	if err := cl.CreateResourceGroup(ctx); err != nil {
		return state.ConstellationState{}, err
	}
	if err := cl.CreateApplicationInsight(ctx); err != nil {
		return state.ConstellationState{}, err
	}
	if err := cl.CreateExternalLoadBalancer(ctx); err != nil {
		return state.ConstellationState{}, err
	}
	if err := cl.CreateVirtualNetwork(ctx); err != nil {
		return state.ConstellationState{}, err
	}

	if err := cl.CreateSecurityGroup(ctx, azurecl.NetworkSecurityGroupInput{
		Ingress: cloudtypes.Firewall(config.IngressFirewall),
		Egress:  cloudtypes.Firewall(config.EgressFirewall),
	}); err != nil {
		return state.ConstellationState{}, err
	}
	createInput := azurecl.CreateInstancesInput{
		CountControlPlanes:   controlPlaneCount,
		CountWorkers:         workerCount,
		InstanceType:         insType,
		StateDiskSizeGB:      config.StateDiskSizeGB,
		StateDiskType:        config.Provider.Azure.StateDiskType,
		Image:                config.Provider.Azure.Image,
		UserAssingedIdentity: config.Provider.Azure.UserAssignedIdentity,
		ConfidentialVM:       *config.Provider.Azure.ConfidentialVM,
	}
	if err := cl.CreateInstances(ctx, createInput); err != nil {
		return state.ConstellationState{}, err
	}

	return cl.GetState(), nil
}
