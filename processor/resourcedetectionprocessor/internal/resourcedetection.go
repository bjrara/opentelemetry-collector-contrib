// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package internal contains an interface for detecting resource information,
// and a provider to merge the resources returned by a slice of custom detectors.
package internal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.uber.org/zap"
)

type DetectorType string

type Detector interface {
	Detect(ctx context.Context) (pdata.Resource, error)
}

type DetectorConfig interface{}

type ResourceDetectorConfig interface {
	GetConfigFromType(DetectorType) DetectorConfig
}

type DetectorFactory func(component.ProcessorCreateParams, DetectorConfig) (Detector, error)

type ResourceProviderFactory struct {
	// detectors holds all possible detector types.
	detectors map[DetectorType]DetectorFactory
}

func NewProviderFactory(detectors map[DetectorType]DetectorFactory) *ResourceProviderFactory {
	return &ResourceProviderFactory{detectors: detectors}
}

func (f *ResourceProviderFactory) CreateResourceProvider(
	params component.ProcessorCreateParams,
	timeout time.Duration,
	detectorConfigs ResourceDetectorConfig,
	detectorTypes ...DetectorType) (*ResourceProvider, error) {
	detectors, err := f.getDetectors(params, detectorConfigs, detectorTypes)
	if err != nil {
		return nil, err
	}

	provider := NewResourceProvider(params.Logger, timeout, detectors...)
	return provider, nil
}

func (f *ResourceProviderFactory) getDetectors(params component.ProcessorCreateParams, detectorConfigs ResourceDetectorConfig, detectorTypes []DetectorType) ([]Detector, error) {
	detectors := make([]Detector, 0, len(detectorTypes))
	for _, detectorType := range detectorTypes {
		detectorFactory, ok := f.detectors[detectorType]
		if !ok {
			return nil, fmt.Errorf("invalid detector key: %v", detectorType)
		}

		detector, err := detectorFactory(params, detectorConfigs.GetConfigFromType(detectorType))
		if err != nil {
			return nil, fmt.Errorf("failed creating detector type %q: %w", detectorType, err)
		}

		detectors = append(detectors, detector)
	}

	return detectors, nil
}

type ResourceProvider struct {
	logger           *zap.Logger
	timeout          time.Duration
	detectors        []Detector
	detectedResource *resourceResult
	once             sync.Once
}

type resourceResult struct {
	resource pdata.Resource
	err      error
}

func NewResourceProvider(logger *zap.Logger, timeout time.Duration, detectors ...Detector) *ResourceProvider {
	return &ResourceProvider{
		logger:    logger,
		timeout:   timeout,
		detectors: detectors,
	}
}

func (p *ResourceProvider) Get(ctx context.Context) (pdata.Resource, error) {
	p.once.Do(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
		p.detectResource(ctx)
	})

	return p.detectedResource.resource, p.detectedResource.err
}

func (p *ResourceProvider) detectResource(ctx context.Context) {
	p.detectedResource = &resourceResult{}

	res := pdata.NewResource()

	p.logger.Info("began detecting resource information")

	for _, detector := range p.detectors {
		r, err := detector.Detect(ctx)
		if err != nil {
			p.detectedResource.err = err
			return
		}

		MergeResource(res, r, false)
	}

	p.logger.Info("detected resource information", zap.Any("resource", AttributesToMap(res.Attributes())))

	p.detectedResource.resource = res
}

func AttributesToMap(am pdata.AttributeMap) map[string]interface{} {
	mp := make(map[string]interface{}, am.Len())
	am.Range(func(k string, v pdata.AttributeValue) bool {
		mp[k] = UnwrapAttribute(v)
		return true
	})
	return mp
}

func UnwrapAttribute(v pdata.AttributeValue) interface{} {
	switch v.Type() {
	case pdata.AttributeValueBOOL:
		return v.BoolVal()
	case pdata.AttributeValueINT:
		return v.IntVal()
	case pdata.AttributeValueDOUBLE:
		return v.DoubleVal()
	case pdata.AttributeValueSTRING:
		return v.StringVal()
	case pdata.AttributeValueARRAY:
		return getSerializableArray(v.ArrayVal())
	case pdata.AttributeValueMAP:
		return AttributesToMap(v.MapVal())
	default:
		return nil
	}
}

func getSerializableArray(inArr pdata.AnyValueArray) []interface{} {
	var outArr []interface{}
	for i := 0; i < inArr.Len(); i++ {
		outArr = append(outArr, UnwrapAttribute(inArr.At(i)))
	}

	return outArr
}

func MergeResource(to, from pdata.Resource, overrideTo bool) {
	if IsEmptyResource(from) {
		return
	}

	toAttr := to.Attributes()
	from.Attributes().Range(func(k string, v pdata.AttributeValue) bool {
		if overrideTo {
			toAttr.Upsert(k, v)
		} else {
			toAttr.Insert(k, v)
		}
		return true
	})
}

func IsEmptyResource(res pdata.Resource) bool {
	return res.Attributes().Len() == 0
}
