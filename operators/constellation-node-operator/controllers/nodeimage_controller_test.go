/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package controllers

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	updatev1alpha1 "github.com/edgelesssys/constellation/operators/constellation-node-operator/api/v1alpha1"
)

func TestAnnotateNodes(t *testing.T) {
	testCases := map[string]struct {
		getScalingGroupErr error
		getNodeImageErr    error
		patchErr           error
		node               corev1.Node
		nodeAfterPatch     corev1.Node
		wantAnnotated      *corev1.Node
	}{
		"node is not annotated": {
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-name",
				},
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
			nodeAfterPatch: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-name",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						nodeImageAnnotation:    "node-image",
					},
				},
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
			wantAnnotated: &corev1.Node{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-name",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						nodeImageAnnotation:    "node-image",
					},
				},
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
		},
		"node is already annotated": {
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						nodeImageAnnotation:    "node-image",
					},
				},
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
			wantAnnotated: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						nodeImageAnnotation:    "node-image",
					},
				},
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
		},
		"node is missing providerID": {},
		"unable to retrieve scaling group": {
			getScalingGroupErr: errors.New("error"),
			node: corev1.Node{
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
		},
		"unable to retrieve node image": {
			getNodeImageErr: errors.New("error"),
			node: corev1.Node{
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
		},
		"unable to patch node annotations": {
			patchErr: errors.New("error"),
			node: corev1.Node{
				Spec: corev1.NodeSpec{ProviderID: "provider-id"},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			reconciler := NodeImageReconciler{
				nodeReplacer: &stubNodeReplacerReader{
					nodeImage:         "node-image",
					scalingGroupID:    "scaling-group-id",
					scalingGroupIDErr: tc.getScalingGroupErr,
					nodeImageErr:      tc.getNodeImageErr,
				},
				Client: &stubReadWriterClient{
					stubReaderClient: *newStubReaderClient(t, []runtime.Object{&tc.nodeAfterPatch}, nil, nil),
					stubWriterClient: stubWriterClient{
						patchErr: tc.patchErr,
					},
				},
			}
			annotated, invalid := reconciler.annotateNodes(context.Background(), []corev1.Node{tc.node})
			if tc.wantAnnotated == nil {
				assert.Len(annotated, 0)
				assert.Len(invalid, 1)
				return
			}

			assert.Len(annotated, 1)
			assert.Len(invalid, 0)
			assert.Equal(*tc.wantAnnotated, annotated[0])
		})
	}
}

func TestPairDonorsAndHeirs(t *testing.T) {
	testCases := map[string]struct {
		outdatedNode corev1.Node
		mintNode     mintNode
		wantPair     *replacementPair
	}{
		"nodes have same scaling group": {
			outdatedNode: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "outdated-name",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
					},
				},
			},
			mintNode: mintNode{
				pendingNode: updatev1alpha1.PendingNode{
					Spec: updatev1alpha1.PendingNodeSpec{
						ScalingGroupID: "scaling-group-id",
					},
				},
				node: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mint-name",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group-id",
						},
					},
				},
			},
			wantPair: &replacementPair{
				donor: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "outdated-name",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group-id",
							heirAnnotation:         "mint-name",
						},
					},
				},
				heir: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mint-name",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group-id",
							donorAnnotation:        "outdated-name",
						},
					},
				},
			},
		},
		"nodes have different scaling groups": {
			outdatedNode: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "outdated-name",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-1",
					},
				},
			},
			mintNode: mintNode{
				pendingNode: updatev1alpha1.PendingNode{
					Spec: updatev1alpha1.PendingNodeSpec{
						ScalingGroupID: "scaling-group-2",
					},
				},
				node: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mint-name",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group-2",
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			reconciler := NodeImageReconciler{
				nodeReplacer: &stubNodeReplacerReader{},
				Client: &stubReadWriterClient{
					stubReaderClient: *newStubReaderClient(t, []runtime.Object{&tc.outdatedNode, &tc.mintNode.node}, nil, nil),
				},
			}
			nodeImage := updatev1alpha1.NodeImage{}
			pairs := reconciler.pairDonorsAndHeirs(context.Background(), &nodeImage, []corev1.Node{tc.outdatedNode}, []mintNode{tc.mintNode})
			if tc.wantPair == nil {
				assert.Len(pairs, 0)
				return
			}

			assert.Len(pairs, 1)
			assert.Equal(*tc.wantPair, pairs[0])
		})
	}
}

func TestMatchDonorsAndHeirs(t *testing.T) {
	testCases := map[string]struct {
		donor, heir corev1.Node
		wantPair    *replacementPair
	}{
		"nodes match": {
			donor: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "donor",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						heirAnnotation:         "heir",
					},
				},
			},
			heir: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "heir",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						donorAnnotation:        "donor",
					},
				},
			},
			wantPair: &replacementPair{
				donor: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "donor",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group-id",
							heirAnnotation:         "heir",
						},
					},
				},
				heir: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "heir",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group-id",
							donorAnnotation:        "donor",
						},
					},
				},
			},
		},
		"nodes do not match": {
			donor: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "donor",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						heirAnnotation:         "other-heir",
					},
				},
			},
			heir: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "heir",
					Annotations: map[string]string{
						scalingGroupAnnotation: "scaling-group-id",
						donorAnnotation:        "other-donor",
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			reconciler := NodeImageReconciler{
				nodeReplacer: &stubNodeReplacerReader{},
				Client: &stubReadWriterClient{
					stubReaderClient: *newStubReaderClient(t, []runtime.Object{&tc.donor, &tc.heir}, nil, nil),
				},
			}
			pairs := reconciler.matchDonorsAndHeirs(context.Background(), nil, []corev1.Node{tc.donor}, []corev1.Node{tc.heir})
			if tc.wantPair == nil {
				assert.Len(pairs, 0)
				return
			}

			assert.Len(pairs, 1)
			assert.Equal(*tc.wantPair, pairs[0])
		})
	}
}

func TestCreateNewNodes(t *testing.T) {
	testCases := map[string]struct {
		outdatedNodes    []corev1.Node
		pendingNodes     []updatev1alpha1.PendingNode
		scalingGroupByID map[string]updatev1alpha1.ScalingGroup
		budget           int
		wantCreateCalls  []string
	}{
		"no outdated nodes": {
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget: 1,
		},
		"single outdated node": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget:          1,
			wantCreateCalls: []string{"scaling-group"},
		},
		"budget larger than needed": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget:          2,
			wantCreateCalls: []string{"scaling-group"},
		},
		"no budget": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
		},
		"scaling group image is outdated": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "outdated-image",
					},
				},
			},
			budget: 1,
		},
		"pending node exists": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			pendingNodes: []updatev1alpha1.PendingNode{
				{
					Spec: updatev1alpha1.PendingNodeSpec{
						Goal:           updatev1alpha1.NodeGoalJoin,
						ScalingGroupID: "scaling-group",
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget: 1,
		},
		"leaving pending node is ignored": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			pendingNodes: []updatev1alpha1.PendingNode{
				{
					Spec: updatev1alpha1.PendingNodeSpec{
						Goal:           updatev1alpha1.NodeGoalLeave,
						ScalingGroupID: "scaling-group",
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget:          1,
			wantCreateCalls: []string{"scaling-group"},
		},
		"freshly chosen donor node is skipped": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
							heirAnnotation:         "heir",
						},
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget: 1,
		},
		"scaling group exists without outdated nodes": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			scalingGroupByID: map[string]updatev1alpha1.ScalingGroup{
				"scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
				"other-scaling-group": {
					Spec: updatev1alpha1.ScalingGroupSpec{
						GroupID: "other-scaling-group",
					},
					Status: updatev1alpha1.ScalingGroupStatus{
						ImageReference: "image",
					},
				},
			},
			budget:          2,
			wantCreateCalls: []string{"scaling-group"},
		},
		"scaling group does not exist": {
			outdatedNodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							scalingGroupAnnotation: "scaling-group",
						},
					},
				},
			},
			budget: 1,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			desiredNodeImage := updatev1alpha1.NodeImage{
				Spec: updatev1alpha1.NodeImageSpec{
					ImageReference: "image",
				},
			}
			reconciler := NodeImageReconciler{
				nodeReplacer: &stubNodeReplacerWriter{},
				Client: &stubReadWriterClient{
					stubReaderClient: *newStubReaderClient(t, []runtime.Object{}, nil, nil),
				},
				Scheme: getScheme(t),
			}
			err := reconciler.createNewNodes(context.Background(), desiredNodeImage, tc.outdatedNodes, tc.pendingNodes, tc.scalingGroupByID, tc.budget)
			require.NoError(err)
			assert.Equal(tc.wantCreateCalls, reconciler.nodeReplacer.(*stubNodeReplacerWriter).createCalls)
		})
	}
}

func TestGroupNodes(t *testing.T) {
	latestImageReference := "latest-image"
	scalingGroup := "scaling-group"
	wantNodeGroups := nodeGroups{
		Outdated: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "outdated",
					Annotations: map[string]string{
						scalingGroupAnnotation: scalingGroup,
						nodeImageAnnotation:    "old-image",
					},
				},
			},
		},
		UpToDate: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "uptodate",
					Annotations: map[string]string{
						scalingGroupAnnotation: scalingGroup,
						nodeImageAnnotation:    latestImageReference,
					},
				},
			},
		},
		Donors: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "donor",
					Annotations: map[string]string{
						scalingGroupAnnotation: scalingGroup,
						nodeImageAnnotation:    "old-image",
						heirAnnotation:         "heir",
					},
				},
			},
		},
		Heirs: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "heir",
					Annotations: map[string]string{
						scalingGroupAnnotation: scalingGroup,
						nodeImageAnnotation:    latestImageReference,
						donorAnnotation:        "donor",
					},
				},
			},
		},
		Obsolete: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "obsolete",
					Annotations: map[string]string{
						scalingGroupAnnotation: scalingGroup,
						nodeImageAnnotation:    latestImageReference,
						obsoleteAnnotation:     "true",
					},
				},
			},
		},
		Mint: []mintNode{
			{
				node: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mint",
						Annotations: map[string]string{
							scalingGroupAnnotation: scalingGroup,
							nodeImageAnnotation:    latestImageReference,
						},
					},
				},
				pendingNode: updatev1alpha1.PendingNode{
					Spec: updatev1alpha1.PendingNodeSpec{
						NodeName: "mint",
						Goal:     updatev1alpha1.NodeGoalJoin,
					},
					Status: updatev1alpha1.PendingNodeStatus{
						CSPNodeState: updatev1alpha1.NodeStateReady,
					},
				},
			},
		},
	}
	nodes := []corev1.Node{}
	nodes = append(nodes, wantNodeGroups.Outdated...)
	nodes = append(nodes, wantNodeGroups.UpToDate...)
	nodes = append(nodes, wantNodeGroups.Donors...)
	nodes = append(nodes, wantNodeGroups.Heirs...)
	nodes = append(nodes, wantNodeGroups.Obsolete...)
	nodes = append(nodes, wantNodeGroups.Mint[0].node)
	pendingNodes := []updatev1alpha1.PendingNode{
		wantNodeGroups.Mint[0].pendingNode,
	}

	assert := assert.New(t)
	groups := groupNodes(nodes, pendingNodes, latestImageReference)
	assert.Equal(wantNodeGroups, groups)
}

type stubNodeReplacer struct {
	sync.RWMutex
	nodeImages        map[string]string
	scalingGroups     map[string]string
	createNodeName    string
	createProviderID  string
	nodeImageErr      error
	scalingGroupIDErr error
	createErr         error
	deleteErr         error
}

func (r *stubNodeReplacer) GetNodeImage(ctx context.Context, providerID string) (string, error) {
	r.RLock()
	defer r.RUnlock()
	return r.nodeImages[providerID], r.nodeImageErr
}

func (r *stubNodeReplacer) GetScalingGroupID(ctx context.Context, providerID string) (string, error) {
	r.RLock()
	defer r.RUnlock()
	return r.scalingGroups[providerID], r.scalingGroupIDErr
}

func (r *stubNodeReplacer) CreateNode(ctx context.Context, scalingGroupID string) (nodeName, providerID string, err error) {
	r.RLock()
	defer r.RUnlock()
	return r.createNodeName, r.createProviderID, r.createErr
}

func (r *stubNodeReplacer) DeleteNode(ctx context.Context, providerID string) error {
	r.RLock()
	defer r.RUnlock()
	return r.deleteErr
}

// thread safe methods to update the stub while in use

func (r *stubNodeReplacer) setNodeImage(providerID, image string) {
	r.Lock()
	defer r.Unlock()
	if r.nodeImages == nil {
		r.nodeImages = make(map[string]string)
	}
	r.nodeImages[providerID] = image
}

func (r *stubNodeReplacer) setScalingGroupID(providerID, scalingGroupID string) {
	r.Lock()
	defer r.Unlock()
	if r.scalingGroups == nil {
		r.scalingGroups = make(map[string]string)
	}
	r.scalingGroups[providerID] = scalingGroupID
}

func (r *stubNodeReplacer) setCreatedNode(nodeName, providerID string, err error) {
	r.Lock()
	defer r.Unlock()
	r.createNodeName = nodeName
	r.createProviderID = providerID
	r.createErr = err
}

type stubNodeReplacerReader struct {
	nodeImage         string
	scalingGroupID    string
	nodeImageErr      error
	scalingGroupIDErr error
	unimplementedNodeReplacer
}

func (r *stubNodeReplacerReader) GetNodeImage(ctx context.Context, providerID string) (string, error) {
	return r.nodeImage, r.nodeImageErr
}

func (r *stubNodeReplacerReader) GetScalingGroupID(ctx context.Context, providerID string) (string, error) {
	return r.scalingGroupID, r.scalingGroupIDErr
}

type stubNodeReplacerWriter struct {
	createNodeName   string
	createProviderID string
	createErr        error
	deleteErr        error

	createCalls []string
	deleteCalls []string

	unimplementedNodeReplacer
}

func (r *stubNodeReplacerWriter) CreateNode(ctx context.Context, scalingGroupID string) (nodeName, providerID string, err error) {
	r.createCalls = append(r.createCalls, scalingGroupID)
	return r.createNodeName, r.createProviderID, r.createErr
}

func (r *stubNodeReplacerWriter) DeleteNode(ctx context.Context, providerID string) error {
	r.deleteCalls = append(r.deleteCalls, providerID)
	return r.deleteErr
}

type unimplementedNodeReplacer struct{}

func (*unimplementedNodeReplacer) GetNodeImage(ctx context.Context, providerID string) (string, error) {
	panic("unimplemented")
}

func (*unimplementedNodeReplacer) GetScalingGroupID(ctx context.Context, providerID string) (string, error) {
	panic("unimplemented")
}

func (*unimplementedNodeReplacer) CreateNode(ctx context.Context, scalingGroupID string) (nodeName, providerID string, err error) {
	panic("unimplemented")
}

func (*unimplementedNodeReplacer) DeleteNode(ctx context.Context, providerID string) error {
	panic("unimplemented")
}
