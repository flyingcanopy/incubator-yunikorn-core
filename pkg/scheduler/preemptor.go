/*
 Licensed to the Apache Software Foundation (ASF) under one
 or more contributor license agreements.  See the NOTICE file
 distributed with this work for additional information
 regarding copyright ownership.  The ASF licenses this file
 to you under the Apache License, Version 2.0 (the
 "License"); you may not use this file except in compliance
 with the License.  You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package scheduler

import (
	"github.com/apache/incubator-yunikorn-core/pkg/common/resources"
)

// Below structures are intended to be used under single go routine, thus no
// lock needed

type preemptionContext struct {
	partitions map[string]*preemptionPartitionContext
}

type preemptionPartitionContext struct {
	partitionTotalResource *resources.Resource
	root                   *preemptionQueueContext
	leafQueues             map[string]*preemptionQueueContext
}

type preemptionQueueContext struct {
	queuePath       string
	schedulingQueue *SchedulingQueue

	// all resources-related for preemption decisions.
	resources *queuePreemptCalcResource

	children map[string]*preemptionQueueContext
	parent   *preemptionQueueContext
}

// resources related to preemption.
type queuePreemptCalcResource struct {
	guaranteed              *resources.Resource
	used                    *resources.Resource
	pending                 *resources.Resource
	max                     *resources.Resource
	ideal                   *resources.Resource
	markedPreemptedResource *resources.Resource
	// How much resource can be preempted by other queues.
	preemptable *resources.Resource
}

func (m *queuePreemptCalcResource) initFromSchedulingQueue(queue *SchedulingQueue) {
	m.guaranteed = queue.QueueInfo.GetGuaranteedResource()
	m.used = queue.QueueInfo.GetAllocatedResource()
	m.pending = queue.GetPendingResource()
	m.max = queue.QueueInfo.GetMaxResource()
}

func newQueuePreemptCalcResource() *queuePreemptCalcResource {
	return &queuePreemptCalcResource{
		ideal:                   resources.NewResource(),
		preemptable:             resources.NewResource(),
		markedPreemptedResource: resources.NewResource(),
	}
}

type PreemptionPolicy interface {
	DoPreemption(scheduler *Scheduler)
}

func getPreemptionPolicies() []PreemptionPolicy {
	preemptionPolicies := make([]PreemptionPolicy, 0)
	preemptionPolicies = append(preemptionPolicies, &DRFPreemptionPolicy{})
	return preemptionPolicies
}

// Visible by tests
func (s *Scheduler) SingleStepPreemption() {
	// Skip if no preemption needed.
	if !s.clusterSchedulingContext.NeedPreemption() {
		return
	}

	s.resetPreemptionContext()

	// Do preemption for each policies
	for _, policy := range getPreemptionPolicies() {
		policy.DoPreemption(s)
	}
}

// Copy & Reset PreemptionContext
func (s *Scheduler) resetPreemptionContext() {
	// Create a new preemption context
	s.preemptionContext = &preemptionContext{
		partitions: make(map[string]*preemptionPartitionContext),
	}

	// Copy from scheduler
	for partition, partitionContext := range s.clusterSchedulingContext.getPartitionMapClone() {
		preemptionPartitionCtx := &preemptionPartitionContext{
			leafQueues: make(map[string]*preemptionQueueContext),
		}
		s.preemptionContext.partitions[partition] = preemptionPartitionCtx
		preemptionPartitionCtx.root = s.recursiveInitPreemptionQueueContext(preemptionPartitionCtx, nil, partitionContext.root)
	}
}

func (s *Scheduler) recursiveInitPreemptionQueueContext(preemptionPartitionCtx *preemptionPartitionContext, parent *preemptionQueueContext,
	queue *SchedulingQueue) *preemptionQueueContext {
	preemptionQueue := &preemptionQueueContext{
		queuePath:       queue.Name,
		parent:          parent,
		schedulingQueue: queue,
		resources:       newQueuePreemptCalcResource(),
		children:        make(map[string]*preemptionQueueContext),
	}

	if queue.isLeafQueue() {
		preemptionPartitionCtx.leafQueues[queue.Name] = preemptionQueue
	}

	for childName, child := range queue.childrenQueues {
		preemptionQueue.children[childName] = s.recursiveInitPreemptionQueueContext(preemptionPartitionCtx, preemptionQueue, child)
	}

	return preemptionQueue
}
