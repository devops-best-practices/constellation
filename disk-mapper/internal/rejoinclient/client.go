/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package rejoinclient

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/edgelesssys/constellation/internal/cloud/metadata"
	"github.com/edgelesssys/constellation/internal/logger"
	"github.com/edgelesssys/constellation/internal/role"
	"github.com/edgelesssys/constellation/joinservice/joinproto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"k8s.io/utils/clock"
)

const (
	interval = 30 * time.Second
	timeout  = 30 * time.Second
)

// RejoinClient is a client for requesting the needed information
// for rejoining a cluster as a restarting worker or control-plane node.
type RejoinClient struct {
	diskUUID string
	nodeInfo metadata.InstanceMetadata

	timeout  time.Duration
	interval time.Duration
	clock    clock.WithTicker

	dialer      grpcDialer
	metadataAPI metadataAPI

	log *logger.Logger
}

// New returns a new RejoinClient.
func New(dial grpcDialer, nodeInfo metadata.InstanceMetadata,
	meta metadataAPI, log *logger.Logger,
) *RejoinClient {
	return &RejoinClient{
		nodeInfo:    nodeInfo,
		timeout:     timeout,
		interval:    interval,
		clock:       clock.RealClock{},
		dialer:      dial,
		metadataAPI: meta,
		log:         log,
	}
}

// Start starts the rejoin client.
// The client will continuously request available control-plane endpoints
// from the metadata API and send rejoin requests to them.
// The function returns after a successful rejoin request has been performed.
func (c *RejoinClient) Start(ctx context.Context, diskUUID string) (diskKey, measurementSecret []byte) {
	c.log.Infof("Starting RejoinClient")
	c.diskUUID = diskUUID
	ticker := c.clock.NewTicker(c.interval)

	defer ticker.Stop()
	defer c.log.Infof("RejoinClient stopped")

	for {
		endpoints, err := c.getControlPlaneEndpoints()
		if err != nil {
			c.log.With(zap.Error(err)).Errorf("Failed to get control-plane endpoints")
		} else {
			c.log.With(zap.Strings("endpoints", endpoints)).Infof("Received list with JoinService endpoints")
			diskKey, measurementSecret, err = c.tryRejoinWithAvailableServices(ctx, endpoints)
			if err == nil {
				c.log.Infof("Successfully retrieved rejoin ticket")
				return diskKey, measurementSecret
			}
		}

		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C():
		}
	}
}

// tryRejoinWithAvailableServices tries sending rejoin requests to the available endpoints.
func (c *RejoinClient) tryRejoinWithAvailableServices(ctx context.Context, endpoints []string) (diskKey, measurementSecret []byte, err error) {
	for _, endpoint := range endpoints {
		c.log.With(zap.String("endpoint", endpoint)).Infof("Requesting rejoin ticket")
		rejoinTicket, err := c.requestRejoinTicket(endpoint)
		if err == nil {
			return rejoinTicket.StateDiskKey, rejoinTicket.MeasurementSecret, nil
		}
		c.log.With(zap.Error(err), zap.String("endpoint", endpoint)).Warnf("Failed to rejoin on endpoint")

		// stop requesting additional endpoints if the context is done
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}
	}
	c.log.Errorf("Failed to rejoin on all endpoints")
	return nil, nil, errors.New("failed to join on all endpoints")
}

// requestRejoinTicket requests a rejoin ticket from the endpoint.
func (c *RejoinClient) requestRejoinTicket(endpoint string) (*joinproto.IssueRejoinTicketResponse, error) {
	ctx, cancel := c.timeoutCtx()
	defer cancel()

	conn, err := c.dialer.Dial(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return joinproto.NewAPIClient(conn).IssueRejoinTicket(ctx, &joinproto.IssueRejoinTicketRequest{DiskUuid: c.diskUUID})
}

// getControlPlaneEndpoints requests the available control-plane endpoints from the metadata API.
// The list is filtered to remove *this* node if it is a restarting control-plane node.
func (c *RejoinClient) getControlPlaneEndpoints() ([]string, error) {
	ctx, cancel := c.timeoutCtx()
	defer cancel()
	endpoints, err := metadata.JoinServiceEndpoints(ctx, c.metadataAPI)
	if err != nil {
		return nil, err
	}
	if c.nodeInfo.Role == role.ControlPlane {
		return removeSelfFromEndpoints(c.nodeInfo.VPCIP, endpoints), nil
	}
	return endpoints, nil
}

// removeSelfFromEndpoints removes *this* node from the list of endpoints.
// If an error occurs, the entry is removed from the list of endpoints.
func removeSelfFromEndpoints(self string, endpoints []string) []string {
	var result []string
	for _, endpoint := range endpoints {
		host, _, err := net.SplitHostPort(endpoint)
		if err == nil && host != self {
			result = append(result, endpoint)
		}
	}
	return result
}

func (c *RejoinClient) timeoutCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.timeout)
}

type grpcDialer interface {
	Dial(ctx context.Context, target string) (*grpc.ClientConn, error)
}

type metadataAPI interface {
	// List retrieves all instances belonging to the current constellation.
	List(ctx context.Context) ([]metadata.InstanceMetadata, error)
}
