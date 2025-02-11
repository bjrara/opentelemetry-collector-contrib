// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows
// +build windows

package hcsshim

import (
	"testing"
	"time"

	"github.com/Microsoft/hcsshim"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	stats "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	ci "github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/containerinsight"
	cTestUtils "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awscontainerinsightreceiver/internal/cadvisor/testutils"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awscontainerinsightreceiver/internal/k8swindows/extractors"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awscontainerinsightreceiver/internal/k8swindows/testutils"
)

// MockKubeletProvider Mock provider implements KubeletProvider interface.
type MockHCSClient struct {
	logger *zap.Logger
	t      *testing.T
}

// MockKubeletProvider Mock provider implements KubeletProvider interface.
type MockKubeletProvider struct {
	logger *zap.Logger
	t      *testing.T
}

func (m *MockHCSClient) GetContainerStats(_ string) (hcsshim.Statistics, error) {
	return hcsshim.Statistics{
		Timestamp: time.Now(),
	}, nil
}

func (m *MockHCSClient) GetEndpointList() ([]hcsshim.HNSEndpoint, error) {
	return []hcsshim.HNSEndpoint{{
		Id:               "endpointId123456c6asdfasdf4354545",
		Name:             "cid-adfklq3qr43lj523l4daf",
		SharedContainers: []string{"1234123412341afasdfa12342343134", "kaljsflasdjf1234123412341afasdfa12342343134"},
	}}, nil
}

func (m *MockHCSClient) GetEndpointStat(_ string) (hcsshim.HNSEndpointStats, error) {
	return hcsshim.HNSEndpointStats{
		BytesReceived:          44340,
		BytesSent:              3432,
		DroppedPacketsIncoming: 43,
		DroppedPacketsOutgoing: 1,
		EndpointID:             "endpointId123456c6asdfasdf4354545",
	}, nil
}

func (m *MockKubeletProvider) GetSummary() (*stats.Summary, error) {
	return testutils.LoadKubeletSummary(m.t, "./../extractors/testdata/CurSingleKubeletSummary.json"), nil
}

func (m *MockKubeletProvider) GetPods() ([]corev1.Pod, error) {
	mockPods := []corev1.Pod{}

	mockPods = append(mockPods, corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "podidq3erqwezdfa3q34q34dfdf",
			Name:      "mockPod",
			Namespace: "default",
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:        "mockContainername",
			ContainerID: "containerd://1234123412341afasdfa12342343134",
		}}},
	})
	return mockPods, nil
}

func createKubeletDecoratorWithMockKubeletProvider(t *testing.T, logger *zap.Logger) Options {
	return func(provider *HCSStatsProvider) {
		provider.kubeletProvider = &MockKubeletProvider{t: t, logger: logger}
	}
}

func createHCSDecoratorWithMockHCSClient(t *testing.T, logger *zap.Logger) Options {
	return func(provider *HCSStatsProvider) {
		provider.hcsClient = &MockHCSClient{t: t, logger: logger}
	}
}

func mockInfoProvider() cTestUtils.MockHostInfo {
	hostInfo := cTestUtils.MockHostInfo{ClusterName: "cluster"}
	return hostInfo
}

func mockMetricExtractors(t *testing.T) []extractors.MetricExtractor {
	metricsExtractors := []extractors.MetricExtractor{}
	metricsExtractors = append(metricsExtractors, extractors.NewNetMetricExtractor(zaptest.NewLogger(t)))
	return metricsExtractors
}

func TestGetContainerToEndpointMap(t *testing.T) {
	hsp, err := NewHnSProvider(zaptest.NewLogger(t), mockInfoProvider(), mockMetricExtractors(t), createKubeletDecoratorWithMockKubeletProvider(t, zaptest.NewLogger(t)), createHCSDecoratorWithMockHCSClient(t, zaptest.NewLogger(t)))
	assert.NoError(t, err)

	containerEndpointMap, err := hsp.getContainerToEndpointMap()

	assert.NoError(t, err)

	assert.Len(t, containerEndpointMap, 2)
	assert.Contains(t, containerEndpointMap, "1234123412341afasdfa12342343134")
	assert.Contains(t, containerEndpointMap, "kaljsflasdjf1234123412341afasdfa12342343134")
}

func TestGetPodToContainerMap(t *testing.T) {
	hsp, err := NewHnSProvider(zaptest.NewLogger(t), mockInfoProvider(), mockMetricExtractors(t), createKubeletDecoratorWithMockKubeletProvider(t, zaptest.NewLogger(t)), createHCSDecoratorWithMockHCSClient(t, zaptest.NewLogger(t)))
	assert.NoError(t, err)

	podContainerMap, err := hsp.getPodToContainerMap()

	assert.NoError(t, err)

	assert.Len(t, podContainerMap, 1)
	assert.Contains(t, podContainerMap, "podidq3erqwezdfa3q34q34dfdf")
	assert.Len(t, podContainerMap["podidq3erqwezdfa3q34q34dfdf"].Containers, 1)
	assert.Equal(t, "1234123412341afasdfa12342343134", podContainerMap["podidq3erqwezdfa3q34q34dfdf"].Containers[0].Id)
}

func TestGetPodMetrics(t *testing.T) {
	hsp, err := NewHnSProvider(zaptest.NewLogger(t), mockInfoProvider(), mockMetricExtractors(t), createKubeletDecoratorWithMockKubeletProvider(t, zaptest.NewLogger(t)), createHCSDecoratorWithMockHCSClient(t, zaptest.NewLogger(t)))
	assert.NoError(t, err)

	containerToEndpointMap, err := hsp.getContainerToEndpointMap()
	assert.NoError(t, err)
	hsp.containerToEndpoint = containerToEndpointMap

	metrics, err := hsp.getPodMetrics()
	assert.NoError(t, err)

	assert.Len(t, metrics, 1)
	podMetric := metrics[0]
	assert.Equal(t, ci.TypePodNet, podMetric.GetMetricType())
	assert.NotNil(t, podMetric.GetTag(ci.PodIDKey))
	assert.NotNil(t, podMetric.GetTag(ci.K8sPodNameKey))
	assert.NotNil(t, podMetric.GetTag(ci.K8sNamespace))
	assert.NotNil(t, podMetric.GetTag(ci.Timestamp))
	assert.NotNil(t, podMetric.GetTag(ci.SourcesKey))
	assert.NotNil(t, podMetric.GetTag(ci.NetIfce))
}
