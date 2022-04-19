package cmd

import (
	"context"
	"errors"

	"github.com/edgelesssys/constellation/coordinator/atls"
	"github.com/edgelesssys/constellation/coordinator/state"
)

type stubStatusWaiter struct {
	initialized   bool
	waitForAllErr error
}

func (s *stubStatusWaiter) InitializeValidators([]atls.Validator) {
	s.initialized = true
}

func (s *stubStatusWaiter) WaitForAll(ctx context.Context, endpoints []string, status ...state.State) error {
	if !s.initialized {
		return errors.New("waiter not initialized")
	}
	return s.waitForAllErr
}
