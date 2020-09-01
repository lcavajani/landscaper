// Copyright 2020 Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package blueprintsregistry_test

import (
	"context"
	"fmt"

	cdv2 "github.com/gardener/component-spec/bindings-go/apis/v2"
	"github.com/go-logr/logr/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"

	lsv1alpha1 "github.com/gardener/landscaper/pkg/apis/core/v1alpha1"
	"github.com/gardener/landscaper/pkg/landscaper/registry/blueprints"
)

const (
	localTestData1 = "./testdata/local-1"
	localTestData2 = "./testdata/local-2"
)

var _ = Describe("Local Registry", func() {

	var (
		fs afero.Fs
	)

	BeforeEach(func() {
		fs = afero.NewMemMapFs()
	})

	Context("initialize Index", func() {
		It("should be successfully initialized with one path", func() {
			_, err := blueprintsregistry.NewLocalRegistry(testing.NullLogger{}, localTestData1)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should be successfully initialized with multiple paths", func() {
			_, err := blueprintsregistry.NewLocalRegistry(testing.NullLogger{}, localTestData1, localTestData2)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should be successfully initialized with multiple paths that are subpaths", func() {
			_, err := blueprintsregistry.NewLocalRegistry(testing.NullLogger{}, localTestData1, fmt.Sprintf("%s/comp1", localTestData1))
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("GetBlueprint", func() {

		var reg blueprintsregistry.Registry

		BeforeEach(func() {
			var err error
			reg, err = blueprintsregistry.NewLocalRegistry(testing.NullLogger{}, localTestData1)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return a component by name", func() {
			_, err := reg.GetBlueprint(context.TODO(), newLocalComponent("root-definition", "1.0.0"))
			Expect(err).ToNot(HaveOccurred())

			_, err = reg.GetBlueprint(context.TODO(), newLocalComponent("sub-definition-1", "1.1.0"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return an error if the name is incorrect", func() {
			_, err := reg.GetBlueprint(context.TODO(), newLocalComponent("unkown-definition", "1.0.0"))
			Expect(blueprintsregistry.IsComponentNotFoundError(err)).To(BeTrue())
		})

		It("should return an error if the version is incorrect", func() {
			_, err := reg.GetBlueprint(context.TODO(), newLocalComponent("sub-definition-1", "1.0.0"))
			Expect(blueprintsregistry.IsVersionNotFoundError(err)).To(BeTrue())
		})
	})

	Context("GetContent", func() {

		var reg blueprintsregistry.Registry

		BeforeEach(func() {
			var err error
			reg, err = blueprintsregistry.NewLocalRegistry(testing.NullLogger{}, localTestData1)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return the blob for a component by name", func() {
			err := reg.GetContent(context.TODO(), newLocalComponent("root-definition", "1.0.0"), fs)
			Expect(err).ToNot(HaveOccurred())

			fs = afero.NewMemMapFs()
			err = reg.GetContent(context.TODO(), newLocalComponent("sub-definition-1", "1.1.0"), fs)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should be able to list all subcomponents as directories int he blob of the root component", func() {
			err := reg.GetContent(context.TODO(), newLocalComponent("root-definition", "1.0.0"), fs)
			Expect(err).ToNot(HaveOccurred())

			dirInfo, err := afero.ReadDir(fs, "/")
			Expect(err).ToNot(HaveOccurred())

			dirs := []string{}
			for _, dir := range dirInfo {
				dirs = append(dirs, dir.Name())
			}

			Expect(dirs).To(And(ContainElement("comp1"), ContainElement("comp1-1")))
		})

		It("should be able to read the test file of the subcomponent", func() {
			err := reg.GetContent(context.TODO(), newLocalComponent("sub-definition-1", "1.1.0"), fs)
			Expect(err).ToNot(HaveOccurred())

			data, err := afero.ReadFile(fs, "/testdata.txt")
			Expect(err).ToNot(HaveOccurred())

			Expect(string(data)).To(Equal("Test Data"))
		})

		It("should return an error if the name is incorrect", func() {
			err := reg.GetContent(context.TODO(), newLocalComponent("unkown-definition", "1.0.0"), fs)
			Expect(blueprintsregistry.IsComponentNotFoundError(err)).To(BeTrue())
		})

		It("should return an error if the version is incorrect", func() {
			err := reg.GetContent(context.TODO(), newLocalComponent("sub-definition-1", "1.0.0"), fs)
			Expect(blueprintsregistry.IsVersionNotFoundError(err)).To(BeTrue())
		})
	})

})

func newLocalComponent(name, version string) cdv2.Resource {
	return cdv2.Resource{
		ObjectMeta: cdv2.ObjectMeta{
			Name:    name,
			Version: version,
		},
		ObjectType: cdv2.ObjectType{Type: lsv1alpha1.BlueprintResourceType},
		Access:     &blueprintsregistry.LocalAccess{ObjectType: cdv2.ObjectType{Type: blueprintsregistry.LocalAccessType}},
	}
}