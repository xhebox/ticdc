// Copyright 2024 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package eventcollector

import (
	"fmt"

	"github.com/flowbehappy/tigate/downstreamadapter/dispatcher"
	"github.com/flowbehappy/tigate/eventpb"
	"github.com/flowbehappy/tigate/pkg/apperror"
	"github.com/flowbehappy/tigate/pkg/common"
	"github.com/flowbehappy/tigate/pkg/common/context"
	"github.com/flowbehappy/tigate/pkg/messaging"
	"github.com/google/uuid"
	"github.com/pingcap/log"
	"go.uber.org/zap"
)

/*
EventCollector is responsible for collecting the events from event service and dispatching them to different dispatchers.
Besides, EventCollector also generate SyncPoint Event for dispatchers when necessary.
EventCollector is an instance-level component.
*/
type EventCollector struct {
	serverId          messaging.ServerId
	dispatcherMap     map[common.DispatcherID]dispatcher.Dispatcher // dispatcher_id --> dispatcher
	globalMemoryQuota int64
}

func NewEventCollector(globalMemoryQuota int64, serverId messaging.ServerId) *EventCollector {
	eventCollector := EventCollector{
		serverId:          serverId,
		globalMemoryQuota: globalMemoryQuota,
		dispatcherMap:     make(map[common.DispatcherID]dispatcher.Dispatcher),
	}
	context.GetService[messaging.MessageCenter](context.MessageCenter).RegisterHandler(messaging.EventFeedTopic, eventCollector.RecvEventsMessage)
	return &eventCollector
}

func (c *EventCollector) RegisterDispatcher(d dispatcher.Dispatcher, startTs uint64) error {
	err := context.GetService[messaging.MessageCenter](context.MessageCenter).SendEvent(&messaging.TargetMessage{
		To:    c.serverId, // demo 中 每个节点都有自己的 eventService
		Topic: messaging.EventServiceTopic,
		Type:  messaging.TypeRegisterDispatcherRequest,
		Message: messaging.RegisterDispatcherRequest{RegisterDispatcherRequest: &eventpb.RegisterDispatcherRequest{
			DispatcherId: uuid.UUID(d.GetId()).String(),
			TableSpan:    d.GetTableSpan().TableSpan,
			Remove:       false,
			StartTs:      startTs,
			ServerId:     c.serverId.String(),
		}},
	})
	if err != nil {
		log.Error("failed to send register dispatcher request message", zap.Error(err))
		return err
	}
	return nil
}

func (c *EventCollector) RemoveDispatcher(d dispatcher.Dispatcher) error {
	err := context.GetService[messaging.MessageCenter](context.MessageCenter).SendEvent(&messaging.TargetMessage{
		To:    c.serverId,
		Topic: messaging.EventServiceTopic,
		Type:  messaging.TypeRegisterDispatcherRequest,
		Message: messaging.RegisterDispatcherRequest{RegisterDispatcherRequest: &eventpb.RegisterDispatcherRequest{
			DispatcherId: uuid.UUID(d.GetId()).String(),
			Remove:       true,
			ServerId:     c.serverId.String(),
			TableSpan:    d.GetTableSpan().TableSpan,
		},
		},
	})
	if err != nil {
		log.Error("failed to send register dispatcher request message", zap.Error(err))
		return err
	}
	return nil
}

func (c *EventCollector) RecvEventsMessage(msg *messaging.TargetMessage) error {
	/*
		if dispatcher.GetGlobalMemoryUsage().UsedBytes > c.globalMemoryQuota {
			// 卡一段时间,怎么拍啊？
			log.Info("downstream adapter is out of memory, waiting for 30 seconds")
			time.Sleep(30 * time.Second)
			continue
		}
	*/

	eventFeeds, ok := msg.Message.(*eventpb.EventFeed)
	log.Info("hello1")
	if !ok {
		log.Error("invalid event feed message", zap.Any("msg", msg))
		return apperror.AppError{Type: apperror.ErrorTypeInvalidMessage, Reason: fmt.Sprintf("invalid heartbeat response message")}
	}

	dispatcherId, err := uuid.Parse(eventFeeds.DispatcherId)
	if err != nil {
		log.Error("invalid dispatcher id", zap.String("dispatcher_id", eventFeeds.DispatcherId))
		return apperror.AppError{Type: apperror.ErrorTypeInvalidMessage, Reason: fmt.Sprintf("invalid dispatcher id: %s", eventFeeds.DispatcherId)}
	}

	if dispatcherItem, ok := c.dispatcherMap[common.DispatcherID(dispatcherId)]; ok {
		// check whether need to update speed ratio
		//ok, ratio := dispatcherItem.GetMemoryUsage().UpdatedSpeedRatio(eventResponse.Ratio)
		// if ok {
		// 	request := eventpb.EventRequest{
		// 		DispatcherId: dispatcherId,
		// 		TableSpan:    dispatcherItem.GetTableSpan(),
		// 		Ratio:        ratio,
		// 		Remove:       false,
		// 	}
		// 	// 这个开销大么，在这里等合适么？看看要不要拆一下
		// 	err := client.Send(&request)
		// 	if err != nil {
		// 		//
		// 	}
		// }

		// if dispatcherId == dispatcher.TableTriggerEventDispatcherId {
		// 	for _, event := range eventResponse.Events {
		// 		dispatcherItem.GetMemoryUsage().Add(event.CommitTs(), event.MemoryCost())
		// 		dispatcherItem.GetEventChan() <- event // 换成一个函数
		// 	}
		// 	dispatcherItem.UpdateResolvedTs(eventResponse.ResolvedTs) // todo:枷锁
		// 	continue
		// }
		if eventFeeds.TableInfo != nil {
			dispatcherItem.(*dispatcher.TableEventDispatcher).InitTableInfo(eventFeeds.TableInfo)
		}
		for _, txnEvent := range eventFeeds.TxnEvents {
			log.Info("hello")
			dispatcherItem.PushEvent(txnEvent)
			/*
				syncPointInfo := dispatcherItem.GetSyncPointInfo()
				// 在这里加 sync point？ 这个性能会有明显影响么,这个要测过
				if syncPointInfo.EnableSyncPoint && event.CommitTs() > syncPointInfo.NextSyncPointTs {
					dispatcherItem.GetEventChan() <- Event{} //构造 Sync Point Event
					syncPointInfo.NextSyncPointTs = oracle.GoTimeToTS(
						oracle.GetTimeFromTS(syncPointInfo.NextSyncPointTs).
							Add(syncPointInfo.SyncPointInterval))
				}
			*/

			// // deal with event
			// dispatcherItem.GetMemoryUsage().Add(event.CommitTs(), event.MemoryCost())
			// dispatcherItem.GetEventChan() <- event // 换成一个函数
		}
		dispatcherItem.UpdateResolvedTs(eventFeeds.ResolvedTs)
	}
	return nil
}
