// Copyright (C) 2017 Google Inc.
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

package vulkan

import (
	"context"

	"github.com/google/gapid/core/log"
	"github.com/google/gapid/core/math/interval"
	"github.com/google/gapid/gapis/api"
	"github.com/google/gapid/gapis/memory"
)

type stateBuilder struct {
	ctx             context.Context
	s               *State
	oldState        *api.GlobalState
	newState        *api.GlobalState
	cmds            []api.Cmd
	cb              CommandBuilder
	readMemories    []*api.AllocResult
	writeMemories   []*api.AllocResult
	memoryIntervals interval.U64RangeList
}

// TODO: wherever possible, use old resources instead of doing full reads on the old pools.
//       This is especially useful for things that are internal pools, (Shader words for example)
func (s *State) RebuildState(ctx context.Context, oldState *api.GlobalState) ([]api.Cmd, interval.U64RangeList) {
	// TODO: Debug Info
	newState := api.NewStateWithAllocator(memory.NewBasicAllocator(oldState.Allocator.FreeList()), oldState.MemoryLayout)
	sb := &stateBuilder{
		ctx:             ctx,
		s:               s,
		oldState:        oldState,
		newState:        newState,
		cb:              CommandBuilder{Thread: 0},
		memoryIntervals: interval.U64RangeList{},
	}
	sb.newState.Memory.NewAt(sb.oldState.Memory.NextPoolID())

	for _, k := range s.Instances.Keys() {
		sb.createInstance(k, s.Instances.Get(k))
	}

	sb.createPhysicalDevices(s.PhysicalDevices)

	for _, su := range s.Surfaces.Keys() {
		sb.createSurface(s.Surfaces.Get(su))
	}

	for _, d := range s.Devices.Keys() {
		sb.createDevice(s.Devices.Get(d))
	}

	for _, q := range s.Queues.Keys() {
		sb.createQueue(s.Queues.Get(q))
	}

	for _, swp := range s.Swapchains.Keys() {
		sb.createSwapchain(s.Swapchains.Get(swp))
	}

	// Create all non-dedicated allocations.
	// Dedicated allocations will be created with their
	// objects
	for _, mem := range s.DeviceMemories.Keys() {
		// TODO: Handle KHR dedicated allocation as well as NV
		sb.createDeviceMemory(s.DeviceMemories.Get(mem), false)
	}

	for _, buf := range s.Buffers.Keys() {
		sb.createBuffer(s.Buffers.Get(buf))
	}

	{
		imgPrimer := newImagePrimer(sb)
		for _, img := range s.Images.Keys() {
			sb.createImage(s.Images.Get(img), imgPrimer)
		}
		imgPrimer.free()
	}

	for _, smp := range s.Samplers.Keys() {
		sb.createSampler(s.Samplers.Get(smp))
	}

	for _, fnc := range s.Fences.Keys() {
		sb.createFence(s.Fences.Get(fnc))
	}

	for _, sem := range s.Semaphores.Keys() {
		sb.createSemaphore(s.Semaphores.Get(sem))
	}

	for _, evt := range s.Events.Keys() {
		sb.createEvent(s.Events.Get(evt))
	}

	for _, cp := range s.CommandPools.Keys() {
		sb.createCommandPool(s.CommandPools.Get(cp))
	}

	for _, pc := range s.PipelineCaches.Keys() {
		sb.createPipelineCache(s.PipelineCaches.Get(pc))
	}

	for _, dsl := range s.DescriptorSetLayouts.Keys() {
		sb.createDescriptorSetLayout(s.DescriptorSetLayouts.Get(dsl))
	}

	for _, pl := range s.PipelineLayouts.Keys() {
		sb.createPipelineLayout(s.PipelineLayouts.Get(pl))
	}

	for _, rp := range s.RenderPasses.Keys() {
		sb.createRenderPass(s.RenderPasses.Get(rp))
	}

	for _, sm := range s.ShaderModules.Keys() {
		sb.createShaderModule(s.ShaderModules.Get(sm))
	}

	for _, cp := range GetPipelinesInOrder(s, true) {
		sb.createComputePipeline(s.ComputePipelines.Get(cp))
	}

	for _, gp := range GetPipelinesInOrder(s, false) {
		sb.createGraphicsPipeline(s.GraphicsPipelines.Get(gp))
	}

	for _, iv := range s.ImageViews.Keys() {
		sb.createImageView(s.ImageViews.Get(iv))
	}

	for _, bv := range s.BufferViews.Keys() {
		sb.createBufferView(s.BufferViews.Get(bv))
	}

	for _, dp := range s.DescriptorPools.Keys() {
		sb.createDescriptorPool(s.DescriptorPools.Get(dp))
	}

	for _, fb := range s.Framebuffers.Keys() {
		sb.createFramebuffer(s.Framebuffers.Get(fb))
	}

	for _, fb := range s.DescriptorSets.Keys() {
		sb.createDescriptorSet(s.DescriptorSets.Get(fb))
	}

	for _, qp := range s.QueryPools.Keys() {
		sb.createQueryPool(s.QueryPools.Get(qp))
	}

	for _, qp := range s.CommandBuffers.Keys() {
		sb.createCommandBuffer(s.CommandBuffers.Get(qp), VkCommandBufferLevel_VK_COMMAND_BUFFER_LEVEL_SECONDARY)
	}

	for _, qp := range s.CommandBuffers.Keys() {
		sb.createCommandBuffer(s.CommandBuffers.Get(qp), VkCommandBufferLevel_VK_COMMAND_BUFFER_LEVEL_PRIMARY)
	}

	return sb.cmds, sb.memoryIntervals
}

func GetPipelinesInOrder(s *State, compute bool) []VkPipeline {

	pipelines := []VkPipeline{}
	unhandledPipelines := map[VkPipeline]VkPipeline{}
	handledPipelines := map[VkPipeline]bool{}
	if compute {
		for _, p := range s.ComputePipelines.Keys() {
			pp := s.ComputePipelines.Get(p)
			unhandledPipelines[pp.VulkanHandle] = pp.BasePipeline
		}
	} else {
		for _, p := range s.GraphicsPipelines.Keys() {
			pp := s.GraphicsPipelines.Get(p)
			unhandledPipelines[pp.VulkanHandle] = pp.BasePipeline
		}
	}

	numHandled := 0
	for len(unhandledPipelines) != 0 {
		for k, v := range unhandledPipelines {
			handled := false
			if v == VkPipeline(0) {
				pipelines = append(pipelines, k)
				handled = true
			} else if _, ok := handledPipelines[v]; ok {
				pipelines = append(pipelines, k)
				handled = true
			}
			if handled {
				handledPipelines[k] = true
				delete(unhandledPipelines, k)
				numHandled++
			}
		}
		if numHandled == 0 {
			// There is a cycle in the basePipeline indices.
			// Or the no base pipelines does exist.
			// Create the rest without base pipelines
			for k, _ := range unhandledPipelines {
				pipelines = append(pipelines, k)
			}
			unhandledPipelines = map[VkPipeline]VkPipeline{}
			break
		}
	}
	return pipelines
}

func (sb *stateBuilder) MustAllocReadData(v ...interface{}) api.AllocResult {
	allocate_result := sb.newState.AllocDataOrPanic(sb.ctx, v...)
	sb.readMemories = append(sb.readMemories, &allocate_result)
	rng := allocate_result.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocate_result
}

func (sb *stateBuilder) MustAllocWriteData(v ...interface{}) api.AllocResult {
	allocate_result := sb.newState.AllocDataOrPanic(sb.ctx, v...)
	sb.writeMemories = append(sb.writeMemories, &allocate_result)
	rng := allocate_result.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocate_result
}

func (sb *stateBuilder) MustUnpackReadMap(v interface{}) api.AllocResult {
	allocate_result, _ := unpackMap(sb.ctx, sb.newState, v)
	sb.readMemories = append(sb.readMemories, &allocate_result)
	rng := allocate_result.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocate_result
}

func (sb *stateBuilder) MustUnpackWriteMap(v interface{}) api.AllocResult {
	allocate_result, _ := unpackMap(sb.ctx, sb.newState, v)
	sb.writeMemories = append(sb.writeMemories, &allocate_result)
	rng := allocate_result.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocate_result
}

func (sb *stateBuilder) getCommandBuffer(queue *QueueObject) (VkCommandBuffer, VkCommandPool) {

	commandBufferId := VkCommandBuffer(newUnusedID(true, func(x uint64) bool { return sb.s.CommandBuffers.Contains(VkCommandBuffer(x)) }))
	commandPoolId := VkCommandPool(newUnusedID(true, func(x uint64) bool { return sb.s.CommandPools.Contains(VkCommandPool(x)) }))

	sb.write(sb.cb.VkCreateCommandPool(
		queue.Device,
		sb.MustAllocReadData(VkCommandPoolCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkCommandPoolCreateFlags(0),
			queue.Family,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(commandPoolId).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkAllocateCommandBuffers(
		queue.Device,
		sb.MustAllocReadData(VkCommandBufferAllocateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			commandPoolId,
			VkCommandBufferLevel_VK_COMMAND_BUFFER_LEVEL_PRIMARY,
			uint32(1),
		}).Ptr(),
		sb.MustAllocWriteData(commandBufferId).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkBeginCommandBuffer(
		commandBufferId,
		sb.MustAllocReadData(VkCommandBufferBeginInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkCommandBufferUsageFlags(0),
			NewVkCommandBufferInheritanceInfoᶜᵖ(memory.Nullptr),
		}).Ptr(),
		VkResult_VK_SUCCESS,
	))

	return commandBufferId, commandPoolId
}

func (sb *stateBuilder) endSubmitAndDestroyCommandBuffer(queue *QueueObject, commandBuffer VkCommandBuffer, commandPool VkCommandPool) {
	sb.write(sb.cb.VkEndCommandBuffer(
		commandBuffer,
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkQueueSubmit(
		queue.VulkanHandle,
		1,
		sb.MustAllocReadData(VkSubmitInfo{
			VkStructureType_VK_STRUCTURE_TYPE_SUBMIT_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			uint32(0),
			NewVkSemaphoreᶜᵖ(memory.Nullptr),
			NewVkPipelineStageFlagsᶜᵖ(memory.Nullptr),
			uint32(1),
			NewVkCommandBufferᶜᵖ(sb.MustAllocReadData(
				commandBuffer,
			).Ptr()),
			uint32(0),
			NewVkSemaphoreᶜᵖ(memory.Nullptr),
		}).Ptr(),
		VkFence(0),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkQueueWaitIdle(queue.VulkanHandle, VkResult_VK_SUCCESS))
	sb.write(sb.cb.VkDestroyCommandPool(
		queue.Device,
		commandPool,
		memory.Nullptr,
	))
}

func (sb *stateBuilder) write(cmd api.Cmd) {
	for _, read := range sb.readMemories {
		cmd.Extras().GetOrAppendObservations().AddRead(read.Data())
	}
	for _, write := range sb.writeMemories {
		cmd.Extras().GetOrAppendObservations().AddWrite(write.Data())
	}

	if err := cmd.Mutate(sb.ctx, api.CmdNoID, sb.newState, nil); err != nil {
		log.W(sb.ctx, "Initial cmd %v: %v - %v", len(sb.cmds), cmd, err)
	} else {
		log.D(sb.ctx, "Initial cmd %v: %v", len(sb.cmds), cmd)
	}
	sb.cmds = append(sb.cmds, cmd)
	for _, read := range sb.readMemories {
		read.Free()
	}
	for _, write := range sb.writeMemories {
		write.Free()
	}
	sb.readMemories = []*api.AllocResult{}
	sb.writeMemories = []*api.AllocResult{}
}

func (sb *stateBuilder) createInstance(vk VkInstance, inst *InstanceObject) {

	enabledLayers := []Charᶜᵖ{}
	for _, layer := range *inst.EnabledLayers.Map {
		enabledLayers = append(enabledLayers, NewCharᶜᵖ(sb.MustAllocReadData(layer).Ptr()))
	}
	enabledExtensions := []Charᶜᵖ{}
	for _, ext := range *inst.EnabledExtensions.Map {
		enabledExtensions = append(enabledExtensions, NewCharᶜᵖ(sb.MustAllocReadData(ext).Ptr()))
	}

	sb.write(sb.cb.VkCreateInstance(
		sb.MustAllocReadData(VkInstanceCreateInfo{
			VkStructureType(VkStructureType_VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO),
			NewVoidᶜᵖ(memory.Nullptr),
			VkInstanceCreateFlags(0),
			NewVkApplicationInfoᶜᵖ(memory.Nullptr),
			uint32(len(*inst.EnabledLayers.Map)),
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledLayers).Ptr()),
			uint32(len(*inst.EnabledExtensions.Map)),
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledExtensions).Ptr()),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(vk).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createPhysicalDevices(Map VkPhysicalDeviceːPhysicalDeviceObjectʳᵐ) {

	devices := map[VkInstance][]VkPhysicalDevice{}
	for _, k := range Map.Keys() {
		v := Map.Get(k)
		_, ok := devices[v.Instance]
		if !ok {
			devices[v.Instance] = []VkPhysicalDevice{}
		}

		devices[v.Instance] = append(devices[v.Instance], k)
	}

	for i, devs := range devices {
		sb.write(sb.cb.VkEnumeratePhysicalDevices(
			i,
			NewU32ᶜᵖ(sb.MustAllocWriteData(len(devs)).Ptr()),
			NewVkPhysicalDeviceᵖ(memory.Nullptr),
			VkResult_VK_SUCCESS,
		))
		sb.write(sb.cb.VkEnumeratePhysicalDevices(
			i,
			NewU32ᶜᵖ(sb.MustAllocReadData(len(devs)).Ptr()),
			NewVkPhysicalDeviceᵖ(sb.MustAllocReadData(devs).Ptr()),
			VkResult_VK_SUCCESS,
		))

		for _, device := range devs {
			pd := Map.Get(device)
			sb.write(sb.cb.VkGetPhysicalDeviceProperties(
				device,
				NewVkPhysicalDevicePropertiesᵖ(sb.MustAllocWriteData(pd.PhysicalDeviceProperties).Ptr()),
			))
			sb.write(sb.cb.VkGetPhysicalDeviceMemoryProperties(
				device,
				NewVkPhysicalDeviceMemoryPropertiesᵖ(sb.MustAllocWriteData(pd.MemoryProperties).Ptr()),
			))
			sb.write(sb.cb.VkGetPhysicalDeviceQueueFamilyProperties(
				device,
				NewU32ᶜᵖ(sb.MustAllocWriteData(len(*pd.QueueFamilyProperties.Map)).Ptr()),
				NewVkQueueFamilyPropertiesᵖ(memory.Nullptr),
			))
			sb.write(sb.cb.VkGetPhysicalDeviceQueueFamilyProperties(
				device,
				NewU32ᶜᵖ(sb.MustAllocReadData(len(*pd.QueueFamilyProperties.Map)).Ptr()),
				NewVkQueueFamilyPropertiesᵖ(sb.MustUnpackWriteMap(*pd.QueueFamilyProperties.Map).Ptr()),
			))
		}
	}
}

func (sb *stateBuilder) createSurface(s *SurfaceObject) {
	switch s.Type {
	case SurfaceType_SURFACE_TYPE_XCB:
		sb.write(sb.cb.VkCreateXcbSurfaceKHR(
			s.Instance,
			sb.MustAllocReadData(VkXcbSurfaceCreateInfoKHR{
				VkStructureType_VK_STRUCTURE_TYPE_XCB_SURFACE_CREATE_INFO_KHR,
				NewVoidᶜᵖ(memory.Nullptr),
				0,
				NewXcbConnectionTᵖ(memory.Nullptr),
				XcbWindowT(0),
			}).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_ANDROID:
		sb.write(sb.cb.VkCreateAndroidSurfaceKHR(
			s.Instance,
			sb.MustAllocReadData(VkAndroidSurfaceCreateInfoKHR{
				VkStructureType_VK_STRUCTURE_TYPE_ANDROID_SURFACE_CREATE_INFO_KHR,
				NewVoidᶜᵖ(memory.Nullptr),
				0,
				NewANativeWindowᵖ(memory.Nullptr),
			}).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_WIN32:
		sb.write(sb.cb.VkCreateWin32SurfaceKHR(
			s.Instance,
			sb.MustAllocReadData(VkWin32SurfaceCreateInfoKHR{
				VkStructureType_VK_STRUCTURE_TYPE_WIN32_SURFACE_CREATE_INFO_KHR,
				NewVoidᶜᵖ(memory.Nullptr),
				0,
				HINSTANCE(0),
				HWND(0),
			}).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_WAYLAND:
		sb.write(sb.cb.VkCreateWaylandSurfaceKHR(
			s.Instance,
			sb.MustAllocReadData(VkWaylandSurfaceCreateInfoKHR{
				VkStructureType_VK_STRUCTURE_TYPE_WAYLAND_SURFACE_CREATE_INFO_KHR,
				NewVoidᶜᵖ(memory.Nullptr),
				0,
				NewWlDisplayᵖ(memory.Nullptr),
				NewWlSurfaceᵖ(memory.Nullptr),
			}).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_XLIB:
		sb.write(sb.cb.VkCreateXlibSurfaceKHR(
			s.Instance,
			sb.MustAllocReadData(VkXlibSurfaceCreateInfoKHR{
				VkStructureType_VK_STRUCTURE_TYPE_XLIB_SURFACE_CREATE_INFO_KHR,
				NewVoidᶜᵖ(memory.Nullptr),
				0,
				NewDisplayᵖ(memory.Nullptr),
				Window(0),
			}).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_MIR:
		sb.write(sb.cb.VkCreateMirSurfaceKHR(
			s.Instance,
			sb.MustAllocReadData(VkMirSurfaceCreateInfoKHR{
				VkStructureType_VK_STRUCTURE_TYPE_MIR_SURFACE_CREATE_INFO_KHR,
				NewVoidᶜᵖ(memory.Nullptr),
				0,
				NewMirConnectionᵖ(memory.Nullptr),
				NewMirSurfaceᵖ(memory.Nullptr),
			}).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle).Ptr(),
			VkResult_VK_SUCCESS,
		))
	}
}

func (sb *stateBuilder) createDevice(d *DeviceObject) {
	enabledLayers := []Charᶜᵖ{}
	for _, layer := range *d.EnabledLayers.Map {
		enabledLayers = append(enabledLayers, NewCharᶜᵖ(sb.MustAllocReadData(layer).Ptr()))
	}
	enabledExtensions := []Charᶜᵖ{}
	for _, ext := range *d.EnabledExtensions.Map {
		enabledExtensions = append(enabledExtensions, NewCharᶜᵖ(sb.MustAllocReadData(ext).Ptr()))
	}

	queue_create := map[uint32]VkDeviceQueueCreateInfo{}
	queue_priorities := map[uint32][]float32{}

	for _, q := range *d.Queues.Map {
		if _, ok := queue_create[q.QueueFamilyIndex]; !ok {
			queue_create[q.QueueFamilyIndex] = VkDeviceQueueCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkDeviceQueueCreateFlags(0),
				q.QueueFamilyIndex,
				0,
				NewF32ᶜᵖ(memory.Nullptr), // This gets filled in later
			}
			queue_priorities[q.QueueFamilyIndex] = []float32{}
		}
		x := queue_create[q.QueueFamilyIndex]
		x.QueueCount += 1
		queue_create[q.QueueFamilyIndex] = x
		if uint32(len(queue_priorities[q.QueueFamilyIndex])) < q.QueueIndex+1 {
			t := make([]float32, q.QueueIndex+1)
			copy(t, queue_priorities[q.QueueFamilyIndex])
			queue_priorities[q.QueueFamilyIndex] = t
		}
		queue_priorities[q.QueueFamilyIndex][q.QueueIndex] = q.Priority
	}
	reordered_queue_creates := map[uint32]VkDeviceQueueCreateInfo{}
	i := uint32(0)
	for k, v := range queue_create {
		v.PQueuePriorities = NewF32ᶜᵖ(sb.MustAllocReadData(queue_priorities[k]).Ptr())
		reordered_queue_creates[i] = v
		i++
	}

	sb.write(sb.cb.VkCreateDevice(
		d.PhysicalDevice,
		sb.MustAllocReadData(VkDeviceCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkDeviceCreateFlags(0),
			uint32(len(reordered_queue_creates)),
			NewVkDeviceQueueCreateInfoᶜᵖ(sb.MustUnpackReadMap(reordered_queue_creates).Ptr()),
			uint32(len(enabledLayers)),
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledLayers).Ptr()),
			uint32(len(enabledExtensions)),
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledExtensions).Ptr()),
			NewVkPhysicalDeviceFeaturesᶜᵖ(sb.MustAllocReadData(d.EnabledFeatures).Ptr()),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(d.VulkanHandle).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createQueue(q *QueueObject) {
	sb.write(sb.cb.VkGetDeviceQueue(
		q.Device,
		q.Family,
		q.Index,
		sb.MustAllocWriteData(q.VulkanHandle).Ptr(),
	))
}

func (sb *stateBuilder) transitionImage(image *ImageObject,
	oldLayout, newLayout VkImageLayout,
	oldQueue, newQueue *QueueObject) {
	if image.LastBoundQueue == nil {
		// We cannot transition an image that has never been
		// on a queue
		return
	}
	commandBuffer, commandPool := sb.getCommandBuffer(image.LastBoundQueue)

	newFamily := newQueue.Family
	oldFamily := newQueue.Family
	if oldQueue != nil {
		oldFamily = oldQueue.Family
	}

	sb.write(sb.cb.VkCmdPipelineBarrier(
		commandBuffer,
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkDependencyFlags(0),
		uint32(0),
		memory.Nullptr,
		uint32(0),
		memory.Nullptr,
		uint32(1),
		sb.MustAllocReadData(VkImageMemoryBarrier{
			VkStructureType_VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER,
			NewVoidᶜᵖ(memory.Nullptr),
			VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT - 1) | VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT),
			VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT - 1) | VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT),
			oldLayout,
			newLayout,
			oldFamily,
			newFamily,
			image.VulkanHandle,
			VkImageSubresourceRange{
				image.ImageAspect,
				uint32(0),
				image.Info.MipLevels,
				uint32(0),
				image.Info.ArrayLayers,
			},
		}).Ptr(),
	))

	sb.endSubmitAndDestroyCommandBuffer(newQueue, commandBuffer, commandPool)
}

func (sb *stateBuilder) createSwapchain(swp *SwapchainObject) {
	extent := VkExtent2D{
		swp.Info.Extent.Width,
		swp.Info.Extent.Height,
	}
	sb.write(sb.cb.VkCreateSwapchainKHR(
		swp.Device,
		sb.MustAllocReadData(VkSwapchainCreateInfoKHR{
			VkStructureType_VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR,
			NewVoidᶜᵖ(memory.Nullptr),
			VkSwapchainCreateFlagsKHR(0),
			swp.Surface.VulkanHandle,
			uint32(len(*swp.SwapchainImages.Map)),
			swp.Info.Format,
			swp.ColorSpace,
			extent,
			swp.Info.ArrayLayers,
			swp.Info.Usage,
			swp.Info.SharingMode,
			uint32(len(*swp.Info.QueueFamilyIndices.Map)),
			NewU32ᶜᵖ(sb.MustUnpackReadMap(*swp.Info.QueueFamilyIndices.Map).Ptr()),
			swp.PreTransform,
			swp.CompositeAlpha,
			swp.PresentMode,
			swp.Clipped,
			VkSwapchainKHR(0),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(swp.VulkanHandle).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkGetSwapchainImagesKHR(
		swp.Device,
		swp.VulkanHandle,
		NewU32ᶜᵖ(sb.MustAllocWriteData(uint32(len(*swp.SwapchainImages.Map))).Ptr()),
		memory.Nullptr,
		VkResult_VK_SUCCESS,
	))

	images := []VkImage{}
	for _, v := range swp.SwapchainImages.Keys() {
		images = append(images, (*swp.SwapchainImages.Map)[v].VulkanHandle)
	}

	sb.write(sb.cb.VkGetSwapchainImagesKHR(
		swp.Device,
		swp.VulkanHandle,
		NewU32ᶜᵖ(sb.MustAllocReadData(uint32(len(*swp.SwapchainImages.Map))).Ptr()),
		sb.MustAllocWriteData(images).Ptr(),
		VkResult_VK_SUCCESS,
	))
	for _, v := range *swp.SwapchainImages.Map {
		q := sb.getQueueFor(v.LastBoundQueue, v.Device, v.Info.QueueFamilyIndices.Map)
		sb.transitionImage(v, VkImageLayout_VK_IMAGE_LAYOUT_UNDEFINED,
			v.Info.Layout, nil, q)
	}
}

func (sb *stateBuilder) createDeviceMemory(mem *DeviceMemoryObject, allowDedicatedNV bool) {
	if !allowDedicatedNV && mem.DedicatedAllocationNV != nil {
		return
	}

	pNext := NewVoidᶜᵖ(memory.Nullptr)

	if mem.DedicatedAllocationNV != nil {
		pNext = NewVoidᶜᵖ(sb.MustAllocReadData(
			VkDedicatedAllocationMemoryAllocateInfoNV{
				VkStructureType_VK_STRUCTURE_TYPE_DEDICATED_ALLOCATION_MEMORY_ALLOCATE_INFO_NV,
				NewVoidᶜᵖ(memory.Nullptr),
				mem.DedicatedAllocationNV.Image,
				mem.DedicatedAllocationNV.Buffer,
			},
		).Ptr())
	}

	sb.write(sb.cb.VkAllocateMemory(
		mem.Device,
		NewVkMemoryAllocateInfoᶜᵖ(sb.MustAllocReadData(
			VkMemoryAllocateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
				pNext,
				mem.AllocationSize,
				mem.MemoryTypeIndex,
			}).Ptr()),
		memory.Nullptr,
		sb.MustAllocWriteData(mem.VulkanHandle).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if mem.MappedLocation.Address() != 0 {
		sb.write(sb.cb.VkMapMemory(
			mem.Device,
			mem.VulkanHandle,
			mem.MappedOffset,
			mem.MappedSize,
			VkMemoryMapFlags(0),
			NewVoidᵖᵖ(sb.MustAllocWriteData(mem.MappedLocation).Ptr()),
			VkResult_VK_SUCCESS,
		))
	}
}

func (sb *stateBuilder) GetScratchBufferMemoryIndex(device *DeviceObject) uint32 {

	physicalDeviceObject := sb.s.PhysicalDevices.Get(device.PhysicalDevice)

	type_bits := uint32((uint64(1) << uint64(physicalDeviceObject.MemoryProperties.MemoryTypeCount)) - 1)
	if sb.s.TransferBufferMemoryRequirements.Contains(device.VulkanHandle) {
		type_bits = sb.s.TransferBufferMemoryRequirements.Get(device.VulkanHandle).MemoryTypeBits
	}
	index := memoryTypeIndexFor(type_bits, &physicalDeviceObject.MemoryProperties, VkMemoryPropertyFlags(VkMemoryPropertyFlagBits_VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT))
	if index >= 0 {
		return uint32(index)
	}
	log.E(sb.ctx, "cannnot get the memory type index for host visible memory to create scratch buffer, fallback to use index 0")
	return 0
}

// Find the index of the memory type that satisfies the specified memory property
// flags.
func memoryTypeIndexFor(memTypeBits uint32, props *VkPhysicalDeviceMemoryProperties, flags VkMemoryPropertyFlags) int {
	for i := uint32(0); i < props.MemoryTypeCount; i++ {
		if (memTypeBits & (1 << i)) == 0 {
			continue
		}
		t := props.MemoryTypes[i]
		if flags == (t.PropertyFlags & flags) {
			return int(i)
		}
	}
	return -1
}

func (sb *stateBuilder) allocAndFillScratchBuffer(device *DeviceObject, data []uint8, usages ...VkBufferUsageFlagBits) (VkBuffer, VkDeviceMemory) {
	buffer := VkBuffer(newUnusedID(true, func(x uint64) bool { return sb.s.Buffers.Contains(VkBuffer(x)) }))
	deviceMemory := VkDeviceMemory(newUnusedID(true, func(x uint64) bool { return sb.s.DeviceMemories.Contains(VkDeviceMemory(x)) }))

	size := VkDeviceSize(len(data))
	usageFlags := VkBufferUsageFlags(VkBufferUsageFlagBits_VK_BUFFER_USAGE_TRANSFER_SRC_BIT)
	for _, u := range usages {
		usageFlags |= VkBufferUsageFlags(u)
	}

	sb.write(sb.cb.VkCreateBuffer(
		device.VulkanHandle,
		sb.MustAllocReadData(
			VkBufferCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkBufferCreateFlags(0),
				size,
				usageFlags,
				VkSharingMode_VK_SHARING_MODE_EXCLUSIVE,
				uint32(0),
				NewU32ᶜᵖ(memory.Nullptr),
			}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(buffer).Ptr(),
		VkResult_VK_SUCCESS,
	))

	memoryTypeIndex := sb.GetScratchBufferMemoryIndex(device)

	// Since we cannot guess how much the driver will actually request of us,
	// overallocate by a factor of 2. This should be enough.
	// Align to 0x100 to make validation layers happy. Assuming the buffer memory
	// requirement has an alignment value compatible with 0x100.
	allocSize := VkDeviceSize((uint64(size*2) + uint64(255)) & ^uint64(255))

	// Make sure we allocate a buffer that is more than big enough for the
	// data
	sb.write(sb.cb.VkAllocateMemory(
		device.VulkanHandle,
		NewVkMemoryAllocateInfoᶜᵖ(sb.MustAllocReadData(
			VkMemoryAllocateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				allocSize,
				memoryTypeIndex,
			}).Ptr()),
		memory.Nullptr,
		sb.MustAllocWriteData(deviceMemory).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkBindBufferMemory(
		device.VulkanHandle,
		buffer,
		deviceMemory,
		0,
		VkResult_VK_SUCCESS,
	))

	dat := sb.newState.AllocDataOrPanic(sb.ctx, data)
	at := NewVoidᵖ(dat.Ptr())
	atdata := sb.newState.AllocDataOrPanic(sb.ctx, at)

	sb.write(sb.cb.VkMapMemory(
		device.VulkanHandle,
		deviceMemory,
		VkDeviceSize(0),
		size,
		VkMemoryMapFlags(0),
		atdata.Ptr(),
		VkResult_VK_SUCCESS,
	).AddRead(atdata.Data()).AddWrite(atdata.Data()))

	sb.write(sb.cb.VkFlushMappedMemoryRanges(
		device.VulkanHandle,
		1,
		sb.MustAllocReadData(VkMappedMemoryRange{
			VkStructureType_VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE,
			NewVoidᶜᵖ(memory.Nullptr),
			deviceMemory,
			VkDeviceSize(0),
			size,
		}).Ptr(),
		VkResult_VK_SUCCESS,
	).AddRead(dat.Data()))

	sb.write(sb.cb.VkUnmapMemory(
		device.VulkanHandle,
		deviceMemory,
	))

	dat.Free()
	atdata.Free()

	return buffer, deviceMemory
}

func (sb *stateBuilder) freeScratchBuffer(device *DeviceObject, buffer VkBuffer, mem VkDeviceMemory) {
	sb.write(sb.cb.VkDestroyBuffer(device.VulkanHandle, buffer, memory.Nullptr))
	sb.write(sb.cb.VkFreeMemory(device.VulkanHandle, mem, memory.Nullptr))
}

func (sb *stateBuilder) getSparseQueueFor(lastBoundQueue *QueueObject, device VkDevice, queueFamilyIndices *map[uint32]uint32) *QueueObject {
	hasQueueFamilyIndices := queueFamilyIndices != nil

	if lastBoundQueue != nil {
		queueProperties := sb.s.PhysicalDevices.Get(sb.s.Devices.Get(lastBoundQueue.Device).PhysicalDevice).QueueFamilyProperties
		if 0 != (uint32(queueProperties.Get(lastBoundQueue.Family).QueueFlags) & uint32(VkQueueFlagBits_VK_QUEUE_SPARSE_BINDING_BIT)) {
			return lastBoundQueue
		}
	}

	dev := sb.s.Devices.Get(device)
	if dev == nil {
		return lastBoundQueue
	}
	phyDev := sb.s.PhysicalDevices.Get(dev.PhysicalDevice)
	if phyDev == nil {
		return lastBoundQueue
	}

	queueProperties := sb.s.PhysicalDevices.Get(sb.s.Devices.Get(device).PhysicalDevice).QueueFamilyProperties

	if hasQueueFamilyIndices {
		for _, v := range *sb.s.Queues.Map {
			if v.Device != device {
				continue
			}
			if 0 != (uint32(queueProperties.Get(v.Family).QueueFlags) & uint32(VkQueueFlagBits_VK_QUEUE_SPARSE_BINDING_BIT)) {
				for _, i := range *queueFamilyIndices {
					if i == v.Family {
						return v
					}
				}
			}
		}
	}
	return lastBoundQueue
}

func (sb *stateBuilder) getQueueFor(lastBoundQueue *QueueObject, device VkDevice, queueFamilyIndices *map[uint32]uint32) *QueueObject {
	if lastBoundQueue != nil {
		return lastBoundQueue
	}
	hasQueueFamilyIndices := queueFamilyIndices != nil

	if hasQueueFamilyIndices {
		for _, v := range *sb.s.Queues.Map {
			if v.Device != device {
				continue
			}
			for _, i := range *queueFamilyIndices {
				if i == v.Family {
					return v
				}
			}
		}
	}

	for _, v := range *sb.s.Queues.Map {
		if v.Device == device {
			return v
		}
	}
	return lastBoundQueue
}

func (sb *stateBuilder) createBuffer(buffer *BufferObject) {
	os := sb.s
	pNext := NewVoidᶜᵖ(memory.Nullptr)

	if buffer.Info.DedicatedAllocationNV != nil {
		pNext = NewVoidᶜᵖ(sb.MustAllocReadData(
			VkDedicatedAllocationBufferCreateInfoNV{
				VkStructureType_VK_STRUCTURE_TYPE_DEDICATED_ALLOCATION_BUFFER_CREATE_INFO_NV,
				NewVoidᶜᵖ(memory.Nullptr),
				buffer.Info.DedicatedAllocationNV.DedicatedAllocation,
			},
		).Ptr())
	}

	denseBound := buffer.Memory != nil
	sparseBound := len(*buffer.SparseMemoryBindings.Map) > 0
	sparseBinding :=
		(uint64(buffer.Info.CreateFlags) &
			uint64(VkBufferCreateFlagBits_VK_BUFFER_CREATE_SPARSE_BINDING_BIT)) != 0
	sparseResidency :=
		sparseBinding &&
			(uint64(buffer.Info.CreateFlags)&
				uint64(VkBufferCreateFlagBits_VK_BUFFER_CREATE_SPARSE_RESIDENCY_BIT)) != 0

	sb.write(sb.cb.VkCreateBuffer(
		buffer.Device,
		sb.MustAllocReadData(
			VkBufferCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
				pNext,
				buffer.Info.CreateFlags,
				buffer.Info.Size,
				VkBufferUsageFlags(uint32(buffer.Info.Usage) | uint32(VkBufferUsageFlagBits_VK_BUFFER_USAGE_TRANSFER_DST_BIT)),
				buffer.Info.SharingMode,
				uint32(len(*buffer.Info.QueueFamilyIndices.Map)),
				NewU32ᶜᵖ(sb.MustUnpackReadMap(*buffer.Info.QueueFamilyIndices.Map).Ptr()),
			}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(buffer.VulkanHandle).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkGetBufferMemoryRequirements(
		buffer.Device,
		buffer.VulkanHandle,
		sb.MustAllocWriteData(buffer.MemoryRequirements).Ptr(),
	))

	// Dedicated allocation buffer/image must NOT be a sparse binding one.
	// Checking the dedicated allocation info on both the memory and the buffer
	// side, because we've found applications that do miss one of them.
	dedicatedMemoryNV := buffer.Memory != nil && (buffer.Info.DedicatedAllocationNV != nil || buffer.Memory.DedicatedAllocationNV != nil)
	// Emit error message to report view if we found one of the dedicate allocation
	// info struct is missing.
	if dedicatedMemoryNV && buffer.Info.DedicatedAllocationNV == nil {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkBuffer", uint64(buffer.VulkanHandle))
	}
	if dedicatedMemoryNV && buffer.Memory.DedicatedAllocationNV == nil {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkDeviceMemory", uint64(buffer.Memory.VulkanHandle))
	}

	if dedicatedMemoryNV {
		sb.createDeviceMemory(buffer.Memory, true)
	}

	if !denseBound && !sparseBound {
		return
	}

	contents := []uint8{}

	copies := []VkBufferCopy{}
	offset := VkDeviceSize(0)

	queue := sb.getQueueFor(buffer.LastBoundQueue, buffer.Device, buffer.Info.QueueFamilyIndices.Map)

	oldFamilyIndex := -1

	if len(*buffer.SparseMemoryBindings.Map) > 0 {
		// If this buffer has sparse memory bindings, then we have to set them all
		// now
		if queue == nil {
			return
		}
		memories := make(map[VkDeviceMemory]bool)
		sparseQueue := sb.getSparseQueueFor(buffer.LastBoundQueue, buffer.Device, buffer.Info.QueueFamilyIndices.Map)
		oldFamilyIndex = int(sparseQueue.Family)
		if buffer.Info.DedicatedAllocationNV != nil {
			for _, bind := range *buffer.SparseMemoryBindings.Map {
				if _, ok := memories[bind.Memory]; !ok {
					memories[bind.Memory] = true
					sb.createDeviceMemory(os.DeviceMemories.Get(bind.Memory), true)
				}
			}
		}

		sb.write(sb.cb.VkQueueBindSparse(
			sparseQueue.VulkanHandle,
			1,
			sb.MustAllocReadData(
				VkBindSparseInfo{
					VkStructureType_VK_STRUCTURE_TYPE_BIND_SPARSE_INFO,
					NewVoidᶜᵖ(memory.Nullptr),
					uint32(0),
					NewVkSemaphoreᶜᵖ(memory.Nullptr),
					uint32(1),
					NewVkSparseBufferMemoryBindInfoᶜᵖ(sb.MustAllocReadData(
						VkSparseBufferMemoryBindInfo{
							buffer.VulkanHandle,
							uint32(len(*buffer.SparseMemoryBindings.Map)),
							NewVkSparseMemoryBindᶜᵖ(
								sb.MustUnpackReadMap(*buffer.SparseMemoryBindings.Map).Ptr(),
							),
						}).Ptr()),
					uint32(0),
					NewVkSparseImageOpaqueMemoryBindInfoᶜᵖ(memory.Nullptr),
					uint32(0),
					NewVkSparseImageMemoryBindInfoᶜᵖ(memory.Nullptr),
					uint32(0),
					NewVkSemaphoreᶜᵖ(memory.Nullptr),
				}).Ptr(),
			VkFence(0),
			VkResult_VK_SUCCESS,
		))
		if sparseResidency || IsFullyBound(0, buffer.Info.Size, buffer.SparseMemoryBindings) {
			for _, bind := range *buffer.SparseMemoryBindings.Map {
				size := bind.Size
				data := sb.s.DeviceMemories.Get(bind.Memory).Data.Slice(
					uint64(bind.MemoryOffset),
					uint64(bind.MemoryOffset+size),
					sb.oldState.MemoryLayout,
				).MustRead(sb.ctx, nil, sb.oldState, nil)
				contents = append(contents, data...)
				copies = append(copies, VkBufferCopy{
					offset, bind.ResourceOffset, size,
				})
				offset += size
				offset = (offset + VkDeviceSize(7)) & (^VkDeviceSize(7))
			}
		}
	} else {
		// Otherwise, we have no sparse bindings, we are either non-sparse, or empty.
		if buffer.Memory == nil {
			return
		}

		sb.write(sb.cb.VkBindBufferMemory(
			buffer.Device,
			buffer.VulkanHandle,
			buffer.Memory.VulkanHandle,
			buffer.MemoryOffset,
			VkResult_VK_SUCCESS,
		))

		size := buffer.Info.Size
		data := buffer.Memory.Data.Slice(
			uint64(buffer.MemoryOffset),
			uint64(buffer.MemoryOffset+size),
			sb.oldState.MemoryLayout,
		).MustRead(sb.ctx, nil, sb.oldState, nil)
		contents = append(contents, data...)
		copies = append(copies, VkBufferCopy{
			offset, 0, size,
		})
	}

	scratchBuffer, scratchMemory := sb.allocAndFillScratchBuffer(
		sb.s.Devices.Get(buffer.Device), contents,
		VkBufferUsageFlagBits_VK_BUFFER_USAGE_TRANSFER_SRC_BIT)

	commandBuffer, commandPool := sb.getCommandBuffer(queue)

	newFamilyIndex := queue.Family

	if oldFamilyIndex == -1 {
		oldFamilyIndex = 0
		newFamilyIndex = 0
	}

	sb.write(sb.cb.VkCmdPipelineBarrier(
		commandBuffer,
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkDependencyFlags(0),
		uint32(0),
		memory.Nullptr,
		uint32(1),
		sb.MustAllocReadData(
			VkBufferMemoryBarrier{
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER,
				NewVoidᶜᵖ(memory.Nullptr),
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT - 1) | VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT),
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT - 1) | VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT),
				uint32(oldFamilyIndex),
				uint32(newFamilyIndex),
				scratchBuffer,
				0,
				VkDeviceSize(len(contents)),
			}).Ptr(),
		uint32(0),
		memory.Nullptr,
	))

	sb.write(sb.cb.VkCmdCopyBuffer(
		commandBuffer,
		scratchBuffer,
		buffer.VulkanHandle,
		uint32(len(copies)),
		sb.MustAllocReadData(copies).Ptr(),
	))

	sb.write(sb.cb.VkCmdPipelineBarrier(
		commandBuffer,
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkDependencyFlags(0),
		uint32(0),
		memory.Nullptr,
		uint32(1),
		sb.MustAllocReadData(
			VkBufferMemoryBarrier{
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER,
				NewVoidᶜᵖ(memory.Nullptr),
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT - 1) | VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT),
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT - 1) | VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT),
				0,
				0,
				buffer.VulkanHandle,
				0,
				VkDeviceSize(len(contents)),
			}).Ptr(),
		uint32(0),
		memory.Nullptr,
	))

	sb.endSubmitAndDestroyCommandBuffer(queue, commandBuffer, commandPool)

	sb.freeScratchBuffer(sb.s.Devices.Get(buffer.Device), scratchBuffer, scratchMemory)
}

func nextMultipleOf8(v uint64) uint64 {
	return (v + 7) & ^uint64(7)
}

type byteSizeAndExtent struct {
	levelSize             uint64
	alignedLevelSize      uint64
	levelSizeInBuf        uint64
	alignedLevelSizeInBuf uint64
	width                 uint64
	height                uint64
	depth                 uint64
}

func (sb *stateBuilder) levelSize(extent VkExtent3D, format VkFormat, mipLevel uint32, aspect VkImageAspectFlagBits) byteSizeAndExtent {
	elementAndTexelBlockSize, _ :=
		subGetElementAndTexelBlockSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, format)
	texelWidth := elementAndTexelBlockSize.TexelBlockSize.Width
	texelHeight := elementAndTexelBlockSize.TexelBlockSize.Height

	width, _ := subGetMipSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, extent.Width, mipLevel)
	height, _ := subGetMipSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, extent.Height, mipLevel)
	depth, _ := subGetMipSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, extent.Depth, mipLevel)
	widthInBlocks, _ := subRoundUpTo(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, width, texelWidth)
	heightInBlocks, _ := subRoundUpTo(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, height, texelHeight)
	elementSize := uint32(0)
	switch aspect {
	case VkImageAspectFlagBits_VK_IMAGE_ASPECT_COLOR_BIT:
		elementSize = elementAndTexelBlockSize.ElementSize
	case VkImageAspectFlagBits_VK_IMAGE_ASPECT_DEPTH_BIT:
		elementSize, _ = subGetDepthElementSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, format, false)
	case VkImageAspectFlagBits_VK_IMAGE_ASPECT_STENCIL_BIT:
		// Stencil element is always 1 byte wide
		elementSize = uint32(1)
	}
	// The Depth element size might be different when it is in buffer instead of image.
	elementSizeInBuf := elementSize
	if aspect == VkImageAspectFlagBits_VK_IMAGE_ASPECT_DEPTH_BIT {
		elementSizeInBuf, _ = subGetDepthElementSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, format, true)
	}

	size := uint64(widthInBlocks) * uint64(heightInBlocks) * uint64(depth) * uint64(elementSize)
	sizeInBuf := uint64(widthInBlocks) * uint64(heightInBlocks) * uint64(depth) * uint64(elementSizeInBuf)

	return byteSizeAndExtent{
		levelSize:             size,
		alignedLevelSize:      nextMultipleOf8(size),
		levelSizeInBuf:        sizeInBuf,
		alignedLevelSizeInBuf: nextMultipleOf8(sizeInBuf),
		width:  uint64(width),
		height: uint64(height),
		depth:  uint64(depth),
	}
}

func (sb *stateBuilder) imageAspectFlagBits(flag VkImageAspectFlags) []VkImageAspectFlagBits {
	bits := []VkImageAspectFlagBits{}
	b, _ := subUnpackImageAspectFlags(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, flag)
	for _, bit := range b.Bits.Range() {
		bits = append(bits, bit)
	}
	return bits
}

// Returns true if the resource range from |offset| with |size| is fully
// covered in the |bindings|.
func IsFullyBound(offset VkDeviceSize, size VkDeviceSize,
	bindings U64ːVkSparseMemoryBindᵐ) bool {
	resourceOffsets := bindings.Keys()

	oneAfterReqRange := -1
	for i := range resourceOffsets {
		if resourceOffsets[i] > uint64(offset+size) {
			oneAfterReqRange = i
			break
		}
	}
	if oneAfterReqRange == -1 || oneAfterReqRange == 0 {
		return false
	}
	i := oneAfterReqRange - 1

	end := offset + size
	for i > 0 && end > offset {
		resOffset := resourceOffsets[i]
		if resOffset+uint64(bindings.Get(resOffset).Size) >= uint64(end) {
			end = VkDeviceSize(resOffset)
			i--
			continue
		}
		return false
	}

	if end <= offset {
		return true
	}

	if i == 0 {
		resOffset := resourceOffsets[0]
		if resOffset <= uint64(offset) &&
			resOffset+uint64(bindings.Get(resOffset).Size) >= uint64(end) {
			return true
		}
	}
	return false
}

func (sb *stateBuilder) createImage(img *ImageObject, imgPrimer *imagePrimer) {
	if img.IsSwapchainImage {
		return
	}

	transDstBit := VkImageUsageFlags(VkImageUsageFlagBits_VK_IMAGE_USAGE_TRANSFER_DST_BIT)
	attBits := VkImageUsageFlags(VkImageUsageFlagBits_VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VkImageUsageFlagBits_VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT)
	storageBit := VkImageUsageFlags(VkImageUsageFlagBits_VK_IMAGE_USAGE_STORAGE_BIT)

	primeByBufCopy := (img.Info.Usage & transDstBit) != VkImageUsageFlags(0)
	primeByRendering := (!primeByBufCopy) && ((img.Info.Usage & attBits) != VkImageUsageFlags(0))
	primeByImageStore := (!primeByBufCopy) && (!primeByRendering) && ((img.Info.Usage & storageBit) != VkImageUsageFlags(0))

	vkCreateImage(sb, img.Device, img.Info, img.VulkanHandle)
	vkGetImageMemoryRequirements(sb, img.Device, img.VulkanHandle, &img.MemoryRequirements)

	denseBound := img.BoundMemory != nil
	sparseBound := len(*img.SparseImageMemoryBindings.Map) > 0 ||
		len(*img.OpaqueSparseMemoryBindings.Map) > 0
	sparseBinding :=
		(uint64(img.Info.Flags) &
			uint64(VkImageCreateFlagBits_VK_IMAGE_CREATE_SPARSE_BINDING_BIT)) != 0
	sparseResidency :=
		sparseBinding &&
			(uint64(img.Info.Flags)&
				uint64(VkImageCreateFlagBits_VK_IMAGE_CREATE_SPARSE_RESIDENCY_BIT)) != 0

	// Dedicated allocation buffer/image must NOT be a sparse binding one.
	// Checking the dedicated allocation info on both the memory and the buffer
	// side, because we've found applications that do miss one of them.
	dedicatedMemoryNV := img.BoundMemory != nil && (img.Info.DedicatedAllocationNV != nil || img.BoundMemory.DedicatedAllocationNV != nil)
	// Emit error message to report view if we found one of the dedicate allocation
	// info struct is missing.
	if dedicatedMemoryNV && img.Info.DedicatedAllocationNV == nil {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkImage", uint64(img.VulkanHandle))
	}
	if dedicatedMemoryNV && img.BoundMemory.DedicatedAllocationNV == nil {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkDeviceMemory", uint64(img.BoundMemory.VulkanHandle))
	}

	if dedicatedMemoryNV {
		sb.createDeviceMemory(img.BoundMemory, true)
	}

	if !denseBound && !sparseBound {
		return
	}

	queue := sb.getQueueFor(img.LastBoundQueue, img.Device, img.Info.QueueFamilyIndices.Map)
	var sparseQueue *QueueObject
	opaqueRanges := []VkImageSubresourceRange{}

	if len(*img.OpaqueSparseMemoryBindings.Map) > 0 || len(*img.SparseImageMemoryBindings.Map) > 0 {
		// If this img has sparse memory bindings, then we have to set them all
		// now
		if queue == nil {
			return
		}
		sparseQueue = sb.getSparseQueueFor(img.LastBoundQueue, img.Device, img.Info.QueueFamilyIndices.Map)
		memories := make(map[VkDeviceMemory]bool)

		nonSparseInfos := []VkSparseImageMemoryBind{}

		for aspect, info := range *img.SparseImageMemoryBindings.Map {
			for layer, layerInfo := range *info.Layers.Map {
				for level, levelInfo := range *layerInfo.Levels.Map {
					for _, block := range *levelInfo.Blocks.Map {
						if img.Info.DedicatedAllocationNV != nil {
							// If this was a dedicated allocation set it here
							if _, ok := memories[block.Memory]; !ok {
								memories[block.Memory] = true
								sb.createDeviceMemory(sb.s.DeviceMemories.Get(block.Memory), true)
							}
							nonSparseInfos = append(nonSparseInfos, VkSparseImageMemoryBind{
								VkImageSubresource{
									VkImageAspectFlags(aspect),
									level,
									layer,
								},
								block.Offset,
								block.Extent,
								block.Memory,
								block.MemoryOffset,
								block.Flags,
							})

						}
					}
				}
			}
		}

		sb.write(sb.cb.VkQueueBindSparse(
			sparseQueue.VulkanHandle,
			1,
			sb.MustAllocReadData(
				VkBindSparseInfo{
					VkStructureType_VK_STRUCTURE_TYPE_BIND_SPARSE_INFO,
					NewVoidᶜᵖ(memory.Nullptr),
					uint32(0),
					NewVkSemaphoreᶜᵖ(memory.Nullptr),
					uint32(0),
					NewVkSparseBufferMemoryBindInfoᶜᵖ(memory.Nullptr),
					uint32(1),
					NewVkSparseImageOpaqueMemoryBindInfoᶜᵖ(sb.MustAllocReadData(
						VkSparseImageOpaqueMemoryBindInfo{
							img.VulkanHandle,
							uint32(len(*img.OpaqueSparseMemoryBindings.Map)),
							NewVkSparseMemoryBindᶜᵖ(
								sb.MustUnpackReadMap(*img.OpaqueSparseMemoryBindings.Map).Ptr(),
							),
						}).Ptr()),
					uint32(0),
					NewVkSparseImageMemoryBindInfoᶜᵖ(sb.MustAllocReadData(
						VkSparseImageMemoryBindInfo{
							img.VulkanHandle,
							uint32(len(nonSparseInfos)),
							NewVkSparseImageMemoryBindᶜᵖ(
								sb.MustAllocReadData(nonSparseInfos).Ptr(),
							),
						}).Ptr()),
					uint32(0),
					NewVkSemaphoreᶜᵖ(memory.Nullptr),
				}).Ptr(),
			VkFence(0),
			VkResult_VK_SUCCESS,
		))

		if sparseResidency {
			isMetadataBound := false
			for _, req := range *img.SparseMemoryRequirements.Map {
				prop := req.FormatProperties
				if uint64(prop.AspectMask)&uint64(VkImageAspectFlagBits_VK_IMAGE_ASPECT_METADATA_BIT) != 0 {
					isMetadataBound = IsFullyBound(req.ImageMipTailOffset, req.ImageMipTailSize, img.OpaqueSparseMemoryBindings)
				}
			}
			if !isMetadataBound {
				// If we have no metadata then the image can have no "real"
				// contents
			} else {
				for _, req := range *img.SparseMemoryRequirements.Map {
					prop := req.FormatProperties
					if (uint64(prop.Flags) & uint64(VkSparseImageFormatFlagBits_VK_SPARSE_IMAGE_FORMAT_SINGLE_MIPTAIL_BIT)) != 0 {
						if !IsFullyBound(req.ImageMipTailOffset, req.ImageMipTailSize, img.OpaqueSparseMemoryBindings) {
							continue
						}
						opaqueRanges = append(opaqueRanges, VkImageSubresourceRange{
							img.ImageAspect,
							req.ImageMipTailFirstLod,
							img.Info.MipLevels - req.ImageMipTailFirstLod,
							0,
							img.Info.ArrayLayers,
						})
					} else {
						for i := uint32(0); i < uint32(img.Info.ArrayLayers); i++ {
							offset := req.ImageMipTailOffset + VkDeviceSize(i)*req.ImageMipTailStride
							if !IsFullyBound(offset, req.ImageMipTailSize, img.OpaqueSparseMemoryBindings) {
								continue
							}
							opaqueRanges = append(opaqueRanges, VkImageSubresourceRange{
								img.ImageAspect,
								req.ImageMipTailFirstLod,
								img.Info.MipLevels - req.ImageMipTailFirstLod,
								i,
								1,
							})
						}
					}
				}
			}
		} else {
			if IsFullyBound(0, img.MemoryRequirements.Size, img.OpaqueSparseMemoryBindings) {
				opaqueRanges = append(opaqueRanges, VkImageSubresourceRange{
					img.ImageAspect,
					0,
					img.Info.MipLevels,
					0,
					img.Info.ArrayLayers,
				})
			}
		}
	} else {
		// Otherwise, we have no sparse bindings, we are either non-sparse, or empty.
		if img.BoundMemory == nil {
			return
		}

		opaqueRanges = append(opaqueRanges, VkImageSubresourceRange{
			img.ImageAspect,
			0,
			img.Info.MipLevels,
			0,
			img.Info.ArrayLayers,
		})
		vkBindImageMemory(sb, img.Device, img.VulkanHandle,
			img.BoundMemory.VulkanHandle, img.BoundMemoryOffset)
	}

	// We won't have to handle UNDEFINED.
	if img.Info.Layout == VkImageLayout_VK_IMAGE_LAYOUT_UNDEFINED {
		return
	}
	// We don't currently prime the data in any of these formats.
	if img.Info.Samples != VkSampleCountFlagBits_VK_SAMPLE_COUNT_1_BIT {
		sb.transitionImage(img, VkImageLayout_VK_IMAGE_LAYOUT_UNDEFINED, img.Info.Layout, sparseQueue, queue)
		log.E(sb.ctx, "[Priming the data of image: %v] priming data for MS images not implemented")
		return
	}
	if img.LastBoundQueue == nil {
		log.W(sb.ctx, "[Priming the data of image: %v] image has never been used on any queue, using arbitrary queue for the priming commands")
	}
	// We have to handle the above cases at some point.
	var err error
	if primeByBufCopy {
		err = imgPrimer.primeByBufferCopy(img, opaqueRanges, queue, sparseQueue)
	} else if primeByRendering {
		err = imgPrimer.primeByRendering(img, opaqueRanges, queue, sparseQueue)
	} else if primeByImageStore {
		err = imgPrimer.primeByImageStore(img, opaqueRanges, queue, sparseQueue)
	}
	if err != nil {
		log.E(sb.ctx, "[Priming the data of image: %v] %v", img.VulkanHandle, err)
	}
	return
}

func (sb *stateBuilder) createSampler(smp *SamplerObject) {
	sb.write(sb.cb.VkCreateSampler(
		smp.Device,
		sb.MustAllocReadData(VkSamplerCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkSamplerCreateFlags(0),
			smp.MagFilter,
			smp.MinFilter,
			smp.MipMapMode,
			smp.AddressModeU,
			smp.AddressModeV,
			smp.AddressModeW,
			smp.MipLodBias,
			smp.AnisotropyEnable,
			smp.MaxAnisotropy,
			smp.CompareEnable,
			smp.CompareOp,
			smp.MinLod,
			smp.MaxLod,
			smp.BorderColor,
			smp.UnnormalizedCoordinates,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			smp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createFence(fnc *FenceObject) {
	flags := VkFenceCreateFlags(0)
	if fnc.Signaled {
		flags = VkFenceCreateFlags(VkFenceCreateFlagBits_VK_FENCE_CREATE_SIGNALED_BIT)
	}
	sb.write(sb.cb.VkCreateFence(
		fnc.Device,
		sb.MustAllocReadData(VkFenceCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_FENCE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			flags,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			fnc.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createSemaphore(sem *SemaphoreObject) {
	sb.write(sb.cb.VkCreateSemaphore(
		sem.Device,
		sb.MustAllocReadData(VkSemaphoreCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkSemaphoreCreateFlags(0),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			sem.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if !sem.Signaled {
		return
	}

	queue := sem.LastQueue
	if !sb.s.Queues.Contains(sem.LastQueue) {
		// find a queue with the same device
		for _, q := range sb.s.Queues.Range() {
			if q.Device == sem.Device {
				queue = q.VulkanHandle
			}
		}
	}

	sb.write(sb.cb.VkQueueSubmit(
		queue,
		1,
		sb.MustAllocReadData(VkSubmitInfo{
			VkStructureType_VK_STRUCTURE_TYPE_SUBMIT_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			0,
			NewVkSemaphoreᶜᵖ(memory.Nullptr),
			NewVkPipelineStageFlagsᶜᵖ(memory.Nullptr),
			0,
			NewVkCommandBufferᶜᵖ(memory.Nullptr),
			1,
			NewVkSemaphoreᶜᵖ(
				sb.MustAllocReadData(sem.VulkanHandle).Ptr(),
			),
		}).Ptr(),
		VkFence(0),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createEvent(evt *EventObject) {
	sb.write(sb.cb.VkCreateEvent(
		evt.Device,
		sb.MustAllocReadData(VkEventCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_EVENT_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkEventCreateFlags(0),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			evt.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if evt.Signaled {
		sb.write(sb.cb.VkSetEvent(
			evt.Device,
			evt.VulkanHandle,
			VkResult_VK_SUCCESS,
		))
	}
}

func (sb *stateBuilder) createCommandPool(cp *CommandPoolObject) {
	sb.write(sb.cb.VkCreateCommandPool(
		cp.Device,
		sb.MustAllocReadData(VkCommandPoolCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			cp.Flags,
			cp.QueueFamilyIndex,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			cp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createPipelineCache(pc *PipelineCacheObject) {
	sb.write(sb.cb.VkCreatePipelineCache(
		pc.Device,
		sb.MustAllocReadData(VkPipelineCacheCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_CACHE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkPipelineCacheCreateFlags(0),
			0,
			NewVoidᶜᵖ(memory.Nullptr),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			pc.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createDescriptorSetLayout(dsl *DescriptorSetLayoutObject) {
	bindings := []VkDescriptorSetLayoutBinding{}
	for _, k := range dsl.Bindings.Keys() {
		b := dsl.Bindings.Get(k)
		smp := NewVkSamplerᶜᵖ(memory.Nullptr)
		if len(*b.ImmutableSamplers.Map) > 0 {
			immutableSamplers := []VkSampler{}
			for _, kk := range b.ImmutableSamplers.Keys() {
				immutableSamplers = append(immutableSamplers, b.ImmutableSamplers.Get(kk).VulkanHandle)
			}
			allocate_result := sb.newState.AllocDataOrPanic(sb.ctx, immutableSamplers)
			sb.readMemories = append(sb.readMemories, &allocate_result)
			smp = NewVkSamplerᶜᵖ(allocate_result.Ptr())
		}

		bindings = append(bindings, VkDescriptorSetLayoutBinding{
			k,
			b.Type,
			b.Count,
			b.Stages,
			smp,
		})
	}

	sb.write(sb.cb.VkCreateDescriptorSetLayout(
		dsl.Device,
		sb.MustAllocReadData(VkDescriptorSetLayoutCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkDescriptorSetLayoutCreateFlags(0),
			uint32(len(bindings)),
			NewVkDescriptorSetLayoutBindingᶜᵖ(
				sb.MustAllocReadData(bindings).Ptr(),
			),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			dsl.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createPipelineLayout(pl *PipelineLayoutObject) {

	descriptorSets := []VkDescriptorSetLayout{}
	for _, k := range pl.SetLayouts.Keys() {
		descriptorSets = append(descriptorSets, pl.SetLayouts.Get(k).VulkanHandle)
	}

	sb.write(sb.cb.VkCreatePipelineLayout(
		pl.Device,
		sb.MustAllocReadData(VkPipelineLayoutCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkPipelineLayoutCreateFlags(0),
			uint32(len(descriptorSets)),
			NewVkDescriptorSetLayoutᶜᵖ(
				sb.MustAllocReadData(descriptorSets).Ptr(),
			),
			uint32(len(*pl.PushConstantRanges.Map)),
			NewVkPushConstantRangeᶜᵖ(sb.MustUnpackReadMap(*pl.PushConstantRanges.Map).Ptr()),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			pl.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createRenderPass(rp *RenderPassObject) {

	subpassDescriptions := []VkSubpassDescription{}
	for _, k := range rp.SubpassDescriptions.Keys() {
		sd := rp.SubpassDescriptions.Get(k)
		depthStencil := NewVkAttachmentReferenceᶜᵖ(memory.Nullptr)
		if sd.DepthStencilAttachment != nil {
			depthStencil = NewVkAttachmentReferenceᶜᵖ(sb.MustAllocReadData(*sd.DepthStencilAttachment).Ptr())
		}
		resolveAttachments := NewVkAttachmentReferenceᶜᵖ(memory.Nullptr)
		if len(*sd.ResolveAttachments.Map) > 0 {
			resolveAttachments = NewVkAttachmentReferenceᶜᵖ(sb.MustUnpackReadMap(*sd.ResolveAttachments.Map).Ptr())
		}

		subpassDescriptions = append(subpassDescriptions, VkSubpassDescription{
			sd.Flags,
			sd.PipelineBindPoint,
			uint32(len(*sd.InputAttachments.Map)),
			NewVkAttachmentReferenceᶜᵖ(sb.MustUnpackReadMap(*sd.InputAttachments.Map).Ptr()),
			uint32(len(*sd.ColorAttachments.Map)),
			NewVkAttachmentReferenceᶜᵖ(sb.MustUnpackReadMap(*sd.ColorAttachments.Map).Ptr()),
			resolveAttachments,
			depthStencil,
			uint32(len(*sd.PreserveAttachments.Map)),
			NewU32ᶜᵖ(sb.MustUnpackReadMap(*sd.PreserveAttachments.Map).Ptr()),
		})
	}

	sb.write(sb.cb.VkCreateRenderPass(
		rp.Device,
		sb.MustAllocReadData(VkRenderPassCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkRenderPassCreateFlags(0),
			uint32(len(*rp.AttachmentDescriptions.Map)),
			NewVkAttachmentDescriptionᶜᵖ(sb.MustUnpackReadMap(*rp.AttachmentDescriptions.Map).Ptr()),
			uint32(len(subpassDescriptions)),
			NewVkSubpassDescriptionᶜᵖ(sb.MustAllocReadData(subpassDescriptions).Ptr()),
			uint32(len(*rp.SubpassDependencies.Map)),
			NewVkSubpassDependencyᶜᵖ(sb.MustUnpackReadMap(*rp.SubpassDependencies.Map).Ptr()),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			rp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createShaderModule(sm *ShaderModuleObject) {
	words := sm.Words.MustRead(sb.ctx, nil, sb.oldState, nil)

	sb.write(sb.cb.VkCreateShaderModule(
		sm.Device,
		sb.MustAllocReadData(VkShaderModuleCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkShaderModuleCreateFlags(0),
			memory.Size(len(words)) * 4,
			NewU32ᶜᵖ(sb.MustAllocReadData(words).Ptr()),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			sm.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createComputePipeline(cp *ComputePipelineObject) {
	cache := VkPipelineCache(0)
	if cp.PipelineCache != nil {
		cache = cp.PipelineCache.VulkanHandle
	}

	basePipeline := VkPipeline(0)
	if cp.BasePipeline != VkPipeline(0) {
		if _, ok := (*GetState(sb.newState).ComputePipelines.Map)[cp.BasePipeline]; ok {
			basePipeline = cp.BasePipeline
		}
	}

	var temporaryShaderModule *ShaderModuleObject

	if !GetState(sb.newState).ShaderModules.Contains(cp.Stage.Module.VulkanHandle) {
		// This is a previously deleted shader module, recreate it, then clear it
		sb.createShaderModule(cp.Stage.Module)
		temporaryShaderModule = cp.Stage.Module
	}

	specializationInfo := NewVkSpecializationInfoᶜᵖ(memory.Nullptr)
	if cp.Stage.Specialization != nil {
		data := cp.Stage.Specialization.Data.MustRead(sb.ctx, nil, sb.oldState, nil)
		specializationInfo = NewVkSpecializationInfoᶜᵖ(sb.MustAllocReadData(VkSpecializationInfo{
			uint32(len(*cp.Stage.Specialization.Specializations.Map)),
			NewVkSpecializationMapEntryᶜᵖ(sb.MustUnpackReadMap(*cp.Stage.Specialization.Specializations.Map).Ptr()),
			memory.Size(len(data)),
			NewVoidᶜᵖ(sb.MustAllocReadData(data).Ptr()),
		}).Ptr())
	}

	sb.write(sb.cb.VkCreateComputePipelines(
		cp.Device,
		cache,
		1,
		sb.MustAllocReadData(VkComputePipelineCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			cp.Flags,
			VkPipelineShaderStageCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineShaderStageCreateFlags(0),
				cp.Stage.Stage,
				cp.Stage.Module.VulkanHandle,
				NewCharᶜᵖ(sb.MustAllocReadData(cp.Stage.EntryPoint).Ptr()),
				specializationInfo,
			},
			cp.PipelineLayout.VulkanHandle,
			basePipeline,
			-1,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			cp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if temporaryShaderModule != nil {
		sb.write(sb.cb.VkDestroyShaderModule(
			temporaryShaderModule.Device,
			temporaryShaderModule.VulkanHandle,
			memory.Nullptr,
		))
	}
}

func (sb *stateBuilder) createGraphicsPipeline(gp *GraphicsPipelineObject) {
	cache := VkPipelineCache(0)
	if gp.PipelineCache != nil {
		cache = gp.PipelineCache.VulkanHandle
	}

	basePipeline := VkPipeline(0)
	if gp.BasePipeline != VkPipeline(0) {
		if _, ok := (*GetState(sb.newState).GraphicsPipelines.Map)[gp.BasePipeline]; ok {
			basePipeline = gp.BasePipeline
		}
	}

	stagesInOrder := gp.Stages.Keys()

	temporaryShaderModules := []*ShaderModuleObject{}
	stages := []VkPipelineShaderStageCreateInfo{}
	for _, ss := range stagesInOrder {
		s := gp.Stages.Get(ss)
		if !GetState(sb.newState).ShaderModules.Contains(s.Module.VulkanHandle) {
			// create temporary shader modules for the pipeline to be created.
			sb.createShaderModule(s.Module)
			temporaryShaderModules = append(temporaryShaderModules, s.Module)
		}
	}

	var temporaryPipelineLayout *PipelineLayoutObject
	if !GetState(sb.newState).PipelineLayouts.Contains(gp.Layout.VulkanHandle) {
		// create temporary pipeline layout for the pipeline to be created.
		sb.createPipelineLayout(gp.Layout)
		temporaryPipelineLayout = GetState(sb.newState).PipelineLayouts.Get(gp.Layout.VulkanHandle)
	}

	var temporaryRenderPass *RenderPassObject
	if !GetState(sb.newState).RenderPasses.Contains(gp.RenderPass.VulkanHandle) {
		// create temporary render pass for the pipeline to be created.
		sb.createRenderPass(gp.RenderPass)
		temporaryRenderPass = GetState(sb.newState).RenderPasses.Get(gp.RenderPass.VulkanHandle)
	}

	// DO NOT! coalesce the prevous calls with this one. createShaderModule()
	// makes calls which means pending read/write observations will get
	// shunted off with it instead of on the VkCreateGraphicsPipelines call
	for _, ss := range stagesInOrder {
		s := gp.Stages.Get(ss)
		specializationInfo := NewVkSpecializationInfoᶜᵖ(memory.Nullptr)
		if s.Specialization != nil {
			data := s.Specialization.Data.MustRead(sb.ctx, nil, sb.oldState, nil)
			specializationInfo = NewVkSpecializationInfoᶜᵖ(sb.MustAllocReadData(VkSpecializationInfo{
				uint32(len(*s.Specialization.Specializations.Map)),
				NewVkSpecializationMapEntryᶜᵖ(sb.MustUnpackReadMap(*s.Specialization.Specializations.Map).Ptr()),
				memory.Size(len(data)),
				NewVoidᶜᵖ(sb.MustAllocReadData(data).Ptr()),
			}).Ptr())
		}
		stages = append(stages, VkPipelineShaderStageCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkPipelineShaderStageCreateFlags(0),
			s.Stage,
			s.Module.VulkanHandle,
			NewCharᶜᵖ(sb.MustAllocReadData(s.EntryPoint).Ptr()),
			specializationInfo,
		})
	}

	tessellationState := NewVkPipelineTessellationStateCreateInfoᶜᵖ(memory.Nullptr)
	if gp.TessellationState != nil {
		tessellationState = NewVkPipelineTessellationStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			VkPipelineTessellationStateCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_TESSELLATION_STATE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineTessellationStateCreateFlags(0),
				gp.TessellationState.PatchControlPoints,
			}).Ptr())
	}

	viewportState := NewVkPipelineViewportStateCreateInfoᶜᵖ(memory.Nullptr)
	if gp.ViewportState != nil {
		viewports := NewVkViewportᶜᵖ(memory.Nullptr)
		if len(*gp.ViewportState.Viewports.Map) > 0 {
			viewports = NewVkViewportᶜᵖ(sb.MustUnpackReadMap(*gp.ViewportState.Viewports.Map).Ptr())
		}
		scissors := NewVkRect2Dᶜᵖ(memory.Nullptr)
		if len(*gp.ViewportState.Scissors.Map) > 0 {
			scissors = NewVkRect2Dᶜᵖ(sb.MustUnpackReadMap(*gp.ViewportState.Scissors.Map).Ptr())
		}

		viewportState = NewVkPipelineViewportStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			VkPipelineViewportStateCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineViewportStateCreateFlags(0),
				gp.ViewportState.ViewportCount,
				viewports,
				gp.ViewportState.ScissorCount,
				scissors,
			}).Ptr())
	}

	multisampleState := NewVkPipelineMultisampleStateCreateInfoᶜᵖ(memory.Nullptr)
	if gp.MultisampleState != nil {
		sampleMask := NewVkSampleMaskᶜᵖ(memory.Nullptr)
		if len(*gp.MultisampleState.SampleMask.Map) > 0 {
			sampleMask = NewVkSampleMaskᶜᵖ(sb.MustUnpackReadMap(*gp.MultisampleState.SampleMask.Map).Ptr())
		}
		multisampleState = NewVkPipelineMultisampleStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			VkPipelineMultisampleStateCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineMultisampleStateCreateFlags(0),
				gp.MultisampleState.RasterizationSamples,
				gp.MultisampleState.SampleShadingEnable,
				gp.MultisampleState.MinSampleShading,
				sampleMask,
				gp.MultisampleState.AlphaToCoverageEnable,
				gp.MultisampleState.AlphaToOneEnable,
			}).Ptr())
	}

	depthState := NewVkPipelineDepthStencilStateCreateInfoᶜᵖ(memory.Nullptr)
	if gp.DepthState != nil {
		depthState = NewVkPipelineDepthStencilStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			VkPipelineDepthStencilStateCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineDepthStencilStateCreateFlags(0),
				gp.DepthState.DepthTestEnable,
				gp.DepthState.DepthWriteEnable,
				gp.DepthState.DepthCompareOp,
				gp.DepthState.DepthBoundsTestEnable,
				gp.DepthState.StencilTestEnable,
				gp.DepthState.Front,
				gp.DepthState.Back,
				gp.DepthState.MinDepthBounds,
				gp.DepthState.MaxDepthBounds,
			}).Ptr())
	}

	colorBlendState := NewVkPipelineColorBlendStateCreateInfoᶜᵖ(memory.Nullptr)
	if gp.ColorBlendState != nil {
		colorblendAttachments := NewVkPipelineColorBlendAttachmentStateᶜᵖ(memory.Nullptr)
		if uint32(len(*gp.ColorBlendState.Attachments.Map)) > 0 {
			colorblendAttachments = NewVkPipelineColorBlendAttachmentStateᶜᵖ(sb.MustUnpackReadMap(*gp.ColorBlendState.Attachments.Map).Ptr())
		}
		colorBlendState = NewVkPipelineColorBlendStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			VkPipelineColorBlendStateCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineColorBlendStateCreateFlags(0),
				gp.ColorBlendState.LogicOpEnable,
				gp.ColorBlendState.LogicOp,
				uint32(len(*gp.ColorBlendState.Attachments.Map)),
				colorblendAttachments,
				gp.ColorBlendState.BlendConstants,
			}).Ptr())
	}

	dynamicState := NewVkPipelineDynamicStateCreateInfoᶜᵖ(memory.Nullptr)
	if gp.DynamicState != nil {
		dynamicStates := NewVkDynamicStateᶜᵖ(memory.Nullptr)
		if uint32(len(*gp.DynamicState.DynamicStates.Map)) > 0 {
			dynamicStates = NewVkDynamicStateᶜᵖ(sb.MustUnpackReadMap(*gp.DynamicState.DynamicStates.Map).Ptr())
		}
		dynamicState = NewVkPipelineDynamicStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			VkPipelineDynamicStateCreateInfo{
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO,
				NewVoidᶜᵖ(memory.Nullptr),
				VkPipelineDynamicStateCreateFlags(0),
				uint32(len(*gp.DynamicState.DynamicStates.Map)),
				dynamicStates,
			}).Ptr())
	}

	sb.write(sb.cb.VkCreateGraphicsPipelines(
		gp.Device,
		cache,
		1,
		sb.MustAllocReadData(VkGraphicsPipelineCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			gp.Flags,
			uint32(len(stages)),
			NewVkPipelineShaderStageCreateInfoᶜᵖ(sb.MustAllocReadData(stages).Ptr()),
			NewVkPipelineVertexInputStateCreateInfoᶜᵖ(sb.MustAllocReadData(
				VkPipelineVertexInputStateCreateInfo{
					VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
					NewVoidᶜᵖ(memory.Nullptr),
					VkPipelineVertexInputStateCreateFlags(0),
					uint32(len(*gp.VertexInputState.BindingDescriptions.Map)),
					NewVkVertexInputBindingDescriptionᶜᵖ(sb.MustUnpackReadMap(*gp.VertexInputState.BindingDescriptions.Map).Ptr()),
					uint32(len(*gp.VertexInputState.AttributeDescriptions.Map)),
					NewVkVertexInputAttributeDescriptionᶜᵖ(sb.MustUnpackReadMap(*gp.VertexInputState.AttributeDescriptions.Map).Ptr()),
				}).Ptr()),
			NewVkPipelineInputAssemblyStateCreateInfoᶜᵖ(sb.MustAllocReadData(
				VkPipelineInputAssemblyStateCreateInfo{
					VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO,
					NewVoidᶜᵖ(memory.Nullptr),
					VkPipelineInputAssemblyStateCreateFlags(0),
					gp.InputAssemblyState.Topology,
					gp.InputAssemblyState.PrimitiveRestartEnable,
				}).Ptr()),
			tessellationState,
			viewportState,
			NewVkPipelineRasterizationStateCreateInfoᶜᵖ(sb.MustAllocReadData(
				VkPipelineRasterizationStateCreateInfo{
					VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
					NewVoidᶜᵖ(memory.Nullptr),
					VkPipelineRasterizationStateCreateFlags(0),
					gp.RasterizationState.DepthClampEnable,
					gp.RasterizationState.RasterizerDiscardEnable,
					gp.RasterizationState.PolygonMode,
					gp.RasterizationState.CullMode,
					gp.RasterizationState.FrontFace,
					gp.RasterizationState.DepthBiasEnable,
					gp.RasterizationState.DepthBiasConstantFactor,
					gp.RasterizationState.DepthBiasClamp,
					gp.RasterizationState.DepthBiasSlopeFactor,
					gp.RasterizationState.LineWidth,
				}).Ptr()),
			multisampleState,
			depthState,
			colorBlendState,
			dynamicState,
			gp.Layout.VulkanHandle,
			gp.RenderPass.VulkanHandle,
			gp.Subpass,
			basePipeline,
			-1,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			gp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	for _, m := range temporaryShaderModules {
		sb.write(sb.cb.VkDestroyShaderModule(
			m.Device,
			m.VulkanHandle,
			memory.Nullptr,
		))
	}

	if temporaryRenderPass != nil {
		sb.write(sb.cb.VkDestroyRenderPass(
			temporaryRenderPass.Device,
			temporaryRenderPass.VulkanHandle,
			memory.Nullptr,
		))
	}

	if temporaryPipelineLayout != nil {
		sb.write(sb.cb.VkDestroyPipelineLayout(
			temporaryPipelineLayout.Device,
			temporaryPipelineLayout.VulkanHandle,
			memory.Nullptr,
		))
	}
}

func (sb *stateBuilder) createImageView(iv *ImageViewObject) {
	if !GetState(sb.newState).Images.Contains(iv.Image.VulkanHandle) {
		// If the image that this image view points to has been deleted,
		// then don't even re-create the image view
		return
	}

	sb.write(sb.cb.VkCreateImageView(
		iv.Device,
		sb.MustAllocReadData(VkImageViewCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkImageViewCreateFlags(0),
			iv.Image.VulkanHandle,
			iv.Type,
			iv.Format,
			iv.Components,
			iv.SubresourceRange,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			iv.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createBufferView(bv *BufferViewObject) {
	if !GetState(sb.newState).Buffers.Contains(bv.Buffer.VulkanHandle) {
		// If the image that this image view points to has been deleted,
		// then don't even re-create the image view
		return
	}

	sb.write(sb.cb.VkCreateBufferView(
		bv.Device,
		sb.MustAllocReadData(VkBufferViewCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_BUFFER_VIEW_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkBufferViewCreateFlags(0),
			bv.Buffer.VulkanHandle,
			bv.Format,
			bv.Offset,
			bv.Range,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			bv.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createDescriptorPool(dp *DescriptorPoolObject) {
	sb.write(sb.cb.VkCreateDescriptorPool(
		dp.Device,
		sb.MustAllocReadData(VkDescriptorPoolCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			dp.Flags,
			dp.MaxSets,
			uint32(len(*dp.Sizes.Map)),
			NewVkDescriptorPoolSizeᶜᵖ(sb.MustUnpackReadMap(*dp.Sizes.Map).Ptr()),
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			dp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createFramebuffer(fb *FramebufferObject) {
	var temporaryRenderPass *RenderPassObject
	if !GetState(sb.newState).RenderPasses.Contains(fb.RenderPass.VulkanHandle) {
		sb.createRenderPass(fb.RenderPass)
		temporaryRenderPass = GetState(sb.newState).RenderPasses.Get(fb.RenderPass.VulkanHandle)
	}

	imageViews := []VkImageView{}
	for _, v := range fb.ImageAttachments.Keys() {
		imageViews = append(imageViews, fb.ImageAttachments.Get(v).VulkanHandle)
	}

	sb.write(sb.cb.VkCreateFramebuffer(
		fb.Device,
		sb.MustAllocReadData(VkFramebufferCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkFramebufferCreateFlags(0),
			fb.RenderPass.VulkanHandle,
			uint32(len(imageViews)),
			NewVkImageViewᶜᵖ(sb.MustAllocReadData(imageViews).Ptr()),
			fb.Width,
			fb.Height,
			fb.Layers,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			fb.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if temporaryRenderPass != nil {
		sb.write(sb.cb.VkDestroyRenderPass(
			temporaryRenderPass.Device,
			temporaryRenderPass.VulkanHandle,
			memory.Nullptr,
		))
	}
}

func (sb *stateBuilder) createDescriptorSet(ds *DescriptorSetObject) {
	ns := GetState(sb.newState)
	if !ns.DescriptorPools.Contains(ds.DescriptorPool) {
		return
	}
	sb.write(sb.cb.VkAllocateDescriptorSets(
		ds.Device,
		sb.MustAllocReadData(VkDescriptorSetAllocateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			ds.DescriptorPool,
			1,
			NewVkDescriptorSetLayoutᶜᵖ(sb.MustAllocReadData(ds.Layout.VulkanHandle).Ptr()),
		}).Ptr(),
		sb.MustAllocWriteData(
			ds.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	writes := []VkWriteDescriptorSet{}
	for _, k := range ds.Bindings.Keys() {
		binding := ds.Bindings.Get(k)
		switch binding.BindingType {
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_SAMPLER:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_IMAGE:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_INPUT_ATTACHMENT:
			numImages := uint32(len(*binding.ImageBinding.Map))
			for i := uint32(0); i < numImages; i++ {
				im := binding.ImageBinding.Get(i)
				if im.Sampler == VkSampler(0) && im.ImageView == VkImageView(0) {
					continue
				}
				if binding.BindingType == VkDescriptorType_VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER &&
					(im.Sampler == VkSampler(0) || im.ImageView == VkImageView(0)) {
					continue
				}
				if im.Sampler != VkSampler(0) && !ns.Samplers.Contains(im.Sampler) {
					log.W(sb.ctx, "Sampler %v is invalid, this descriptor[%v] will remain empty", im.Sampler, ds.VulkanHandle)
					continue
				}
				if im.ImageView != VkImageView(0) && !ns.ImageViews.Contains(im.ImageView) {
					log.W(sb.ctx, "ImageView %v is invalid, this descriptor[%v] will remain empty", im.Sampler, ds.VulkanHandle)
					continue
				}

				writes = append(writes, VkWriteDescriptorSet{
					VkStructureType_VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
					NewVoidᶜᵖ(memory.Nullptr),
					ds.VulkanHandle,
					k,
					i,
					1,
					binding.BindingType,
					NewVkDescriptorImageInfoᶜᵖ(sb.MustAllocReadData(*im).Ptr()),
					NewVkDescriptorBufferInfoᶜᵖ(memory.Nullptr),
					NewVkBufferViewᶜᵖ(memory.Nullptr),
				})
			}
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_BUFFER:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_BUFFER_DYNAMIC:
			numBuffers := uint32(len(*binding.BufferBinding.Map))
			for i := uint32(0); i < numBuffers; i++ {
				buff := binding.BufferBinding.Get(i)
				if buff.Buffer == VkBuffer(0) {
					continue
				}
				if buff.Buffer != VkBuffer(0) && !ns.Buffers.Contains(buff.Buffer) {
					log.W(sb.ctx, "Buffer %v is invalid, this descriptor[%v] will remain empty", buff.Buffer, ds.VulkanHandle)
					continue
				}
				writes = append(writes, VkWriteDescriptorSet{
					VkStructureType_VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
					NewVoidᶜᵖ(memory.Nullptr),
					ds.VulkanHandle,
					k,
					i,
					1,
					binding.BindingType,
					NewVkDescriptorImageInfoᶜᵖ(memory.Nullptr),
					NewVkDescriptorBufferInfoᶜᵖ(sb.MustAllocReadData(*buff).Ptr()),
					NewVkBufferViewᶜᵖ(memory.Nullptr),
				})
			}
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_UNIFORM_TEXEL_BUFFER:
			fallthrough
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_TEXEL_BUFFER:
			numBuffers := uint32(len(*binding.BufferViewBindings.Map))
			for i := uint32(0); i < numBuffers; i++ {
				bv := binding.BufferViewBindings.Get(i)
				if bv == VkBufferView(0) {
					continue
				}
				if bv != VkBufferView(0) && !ns.BufferViews.Contains(bv) {
					log.W(sb.ctx, "BufferView %v is invalid, this descriptor[%v] will remain empty", bv, ds.VulkanHandle)
					continue
				}
				writes = append(writes, VkWriteDescriptorSet{
					VkStructureType_VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
					NewVoidᶜᵖ(memory.Nullptr),
					ds.VulkanHandle,
					k,
					i,
					1,
					binding.BindingType,
					NewVkDescriptorImageInfoᶜᵖ(memory.Nullptr),
					NewVkDescriptorBufferInfoᶜᵖ(memory.Nullptr),
					NewVkBufferViewᶜᵖ(sb.MustAllocReadData(bv).Ptr()),
				})
			}
		}
	}
	sb.write(sb.cb.VkUpdateDescriptorSets(
		ds.Device,
		uint32(len(writes)),
		sb.MustAllocReadData(writes).Ptr(),
		0,
		memory.Nullptr,
	))
}

func (sb *stateBuilder) createQueryPool(qp *QueryPoolObject) {
	sb.write(sb.cb.VkCreateQueryPool(
		qp.Device,
		sb.MustAllocReadData(VkQueryPoolCreateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			VkQueryPoolCreateFlags(0),
			qp.QueryType,
			qp.QueryCount,
			qp.PipelineStatistics,
		}).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(
			qp.VulkanHandle,
		).Ptr(),
		VkResult_VK_SUCCESS,
	))

	anyActive := false
	for _, k := range *qp.Status.Map {
		if k != QueryStatus_QUERY_STATUS_INACTIVE {
			anyActive = true
			break
		}
	}
	if !anyActive {
		return
	}
	queue := sb.getQueueFor(nil, qp.Device, nil)

	commandBuffer, commandPool := sb.getCommandBuffer(queue)
	for i := uint32(0); i < qp.QueryCount; i++ {
		if qp.Status.Get(i) != QueryStatus_QUERY_STATUS_INACTIVE {
			sb.write(sb.cb.VkCmdBeginQuery(
				commandBuffer,
				qp.VulkanHandle,
				i,
				VkQueryControlFlags(0)))
		}
		if qp.Status.Get(i) == QueryStatus_QUERY_STATUS_COMPLETE {
			sb.write(sb.cb.VkCmdEndQuery(
				commandBuffer,
				qp.VulkanHandle,
				i))
		}
	}

	sb.endSubmitAndDestroyCommandBuffer(queue, commandBuffer, commandPool)
}

func (sb *stateBuilder) createCommandBuffer(cb *CommandBufferObject, level VkCommandBufferLevel) {
	if cb.Level != level {
		return
	}

	sb.write(sb.cb.VkAllocateCommandBuffers(
		cb.Device,
		sb.MustAllocReadData(VkCommandBufferAllocateInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			cb.Pool,
			cb.Level,
			uint32(1),
		}).Ptr(),
		sb.MustAllocWriteData(cb.VulkanHandle).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if cb.Recording == RecordingState_NOT_STARTED {
		return
	}

	beginInfo := VkCommandBufferBeginInfo{
		VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO,
		NewVoidᶜᵖ(memory.Nullptr),
		VkCommandBufferUsageFlags(cb.BeginInfo.Flags),
		NewVkCommandBufferInheritanceInfoᶜᵖ(memory.Nullptr),
	}
	if cb.BeginInfo.Inherited {
		inheritanceInfo := sb.MustAllocReadData(VkCommandBufferInheritanceInfo{
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_INHERITANCE_INFO,
			NewVoidᶜᵖ(memory.Nullptr),
			cb.BeginInfo.InheritedRenderPass,
			cb.BeginInfo.InheritedSubpass,
			cb.BeginInfo.InheritedFramebuffer,
			cb.BeginInfo.InheritedOcclusionQuery,
			cb.BeginInfo.InheritedQueryFlags,
			cb.BeginInfo.InheritedPipelineStatsFlags,
		})
		beginInfo.PInheritanceInfo = NewVkCommandBufferInheritanceInfoᶜᵖ(inheritanceInfo.Ptr())
	}

	sb.write(sb.cb.VkBeginCommandBuffer(
		cb.VulkanHandle,
		sb.MustAllocReadData(beginInfo).Ptr(),
		VkResult_VK_SUCCESS,
	))

	hasError := false
	// fill command buffer
	for i := uint32(0); i < uint32(len(*cb.CommandReferences.Map)); i++ {
		arg := GetCommandArgs(sb.ctx, cb.CommandReferences.Get(i), GetState(sb.oldState))
		cleanup, cmd, err := AddCommand(sb.ctx, sb.cb, cb.VulkanHandle, sb.oldState, sb.newState, arg)
		if err != nil {
			log.W(sb.ctx, "Command Buffer %v is invalid, it will not be recorded: - %v", cb.VulkanHandle, err)
			hasError = true
			break
		}
		sb.write(cmd)
		cleanup()
	}
	if hasError {
		return
	}
	if cb.Recording == RecordingState_COMPLETED {
		sb.write(sb.cb.VkEndCommandBuffer(
			cb.VulkanHandle,
			VkResult_VK_SUCCESS,
		))
	}
}
