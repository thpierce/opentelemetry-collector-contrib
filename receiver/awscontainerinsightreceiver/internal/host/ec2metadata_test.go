// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"errors"
	"testing"
	"time"

	awsec2metadata "github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type mockMetadataClient struct {
	count int
}

func (m *mockMetadataClient) GetInstanceIdentityDocument() (awsec2metadata.EC2InstanceIdentityDocument, error) {
	m.count++
	if m.count == 1 {
		return awsec2metadata.EC2InstanceIdentityDocument{}, errors.New("error")
	}

	return awsec2metadata.EC2InstanceIdentityDocument{
		Region:       "us-west-2",
		InstanceID:   "i-abcd1234",
		InstanceType: "c4.xlarge",
		PrivateIP:    "79.168.255.0",
	}, nil
}

func (m *mockMetadataClient) GetMetadata(_ string) (string, error) {
	return "eni-001", nil
}

func TestEC2Metadata(t *testing.T) {
	ctx := t.Context()
	sess := mock.Session
	instanceIDReadyC := make(chan bool)
	instanceIPReadyP := make(chan bool)
	clientOption := func(e *ec2Metadata) {
		e.client = &mockMetadataClient{}
		e.clientFallbackEnable = &mockMetadataClient{}
	}
	e := newEC2Metadata(ctx, sess, 3*time.Millisecond, instanceIDReadyC, instanceIPReadyP, false, 0, zap.NewNop(), nil, clientOption)
	assert.NotNil(t, e)

	<-instanceIDReadyC
	<-instanceIPReadyP
	assert.Equal(t, "i-abcd1234", e.getInstanceID())
	assert.Equal(t, "c4.xlarge", e.getInstanceType())
	assert.Equal(t, "us-west-2", e.getRegion())
	assert.Equal(t, "79.168.255.0", e.getInstanceIP())
	eniID, err := e.getNetworkInterfaceID("00:00:00:00:00:01")
	assert.NoError(t, err)
	assert.Equal(t, "eni-001", eniID)
}
