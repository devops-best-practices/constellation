package main

import (
	"net"

	"github.com/edgelesssys/constellation/bootstrapper/internal/clean"
	"github.com/edgelesssys/constellation/bootstrapper/internal/diskencryption"
	"github.com/edgelesssys/constellation/bootstrapper/internal/initserver"
	"github.com/edgelesssys/constellation/bootstrapper/internal/joinclient"
	"github.com/edgelesssys/constellation/bootstrapper/internal/logging"
	"github.com/edgelesssys/constellation/bootstrapper/internal/nodelock"
	"github.com/edgelesssys/constellation/internal/attestation/vtpm"
	"github.com/edgelesssys/constellation/internal/file"
	"github.com/edgelesssys/constellation/internal/grpc/dialer"
	"github.com/edgelesssys/constellation/internal/logger"
	"github.com/edgelesssys/constellation/internal/oid"
	"go.uber.org/zap"
)

var version = "0.0.0"

func run(issuer quoteIssuer, tpm vtpm.TPMOpenFunc, fileHandler file.Handler,
	kube clusterInitJoiner, metadata joinclient.MetadataAPI,
	bindIP, bindPort string, log *logger.Logger,
	cloudLogger logging.CloudLogger,
) {
	defer cloudLogger.Close()

	log.With(zap.String("version", version)).Infof("Starting bootstrapper")
	cloudLogger.Disclose("bootstrapper started running...")

	uuid, err := getDiskUUID()
	if err != nil {
		log.With(zap.Error(err)).Errorf("Failed to get disk UUID")
		cloudLogger.Disclose("Failed to get disk UUID")
	} else {
		log.Infof("Disk UUID: %s", uuid)
		cloudLogger.Disclose("Disk UUID: " + uuid)
	}

	nodeBootstrapped, err := vtpm.IsNodeBootstrapped(tpm)
	if err != nil {
		log.With(zap.Error(err)).Fatalf("Failed to check if node was previously bootstrapped")
	}

	if nodeBootstrapped {
		if err := kube.StartKubelet(); err != nil {
			log.With(zap.Error(err)).Fatalf("Failed to restart kubelet")
		}
		return
	}

	nodeLock := nodelock.New(tpm)
	initServer := initserver.New(nodeLock, kube, issuer, fileHandler, log)

	dialer := dialer.New(issuer, nil, &net.Dialer{})
	joinClient := joinclient.New(nodeLock, dialer, kube, metadata, log)

	cleaner := clean.New().With(initServer).With(joinClient)
	go cleaner.Start()
	defer cleaner.Done()

	joinClient.Start(cleaner)

	if err := initServer.Serve(bindIP, bindPort, cleaner); err != nil {
		log.With(zap.Error(err)).Fatalf("Failed to serve init server")
	}

	log.Infof("bootstrapper done")
	cloudLogger.Disclose("bootstrapper done")
}

func getDiskUUID() (string, error) {
	disk := diskencryption.New()
	if err := disk.Open(); err != nil {
		return "", err
	}
	defer disk.Close()
	return disk.UUID()
}

type clusterInitJoiner interface {
	joinclient.ClusterJoiner
	initserver.ClusterInitializer
	StartKubelet() error
}

type quoteIssuer interface {
	oid.Getter
	// Issue issues a quote for remote attestation for a given message
	Issue(userData []byte, nonce []byte) (quote []byte, err error)
}
