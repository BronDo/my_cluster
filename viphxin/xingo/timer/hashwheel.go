package timer

import (
	"errors"
	"fmt"
	"github.com/viphxin/xingo/logger"
	//	"runtime/debug"
	"sync"
	"time"
)

/*
分级时间轮
*/

const (
	DEFAULT_LEVEL = 12
)

type HashWheel struct {
	title         string                        //时间轮唯一标识
	index         int                           //时间轮当前指针
	level         int                           //多少级
	levelInterval int64                         //分级间隔 (ms)
	maxCap        uint32                        //每一级最大容量
	timerQueue    map[int]map[uint32]*SafeTimer //存储所有timer
	nextHashWheel *HashWheel                    //下级时间轮
	sync.RWMutex
}

func NewHashWheel(title string, level int, linterval int64, maxCap uint32) *HashWheel {
	wheel := &HashWheel{
		title:         title,
		index:         0,
		level:         level,
		levelInterval: linterval,
		maxCap:        maxCap,
		timerQueue:    make(map[int]map[uint32]*SafeTimer, level),
	}
	for i := 0; i < wheel.level; i++ {
		wheel.timerQueue[i] = make(map[uint32]*SafeTimer, maxCap)
	}
	go wheel.RunWheel()
	return wheel
}

func (this *HashWheel) AddNext(next *HashWheel) {
	this.nextHashWheel = next
}

func (this *HashWheel) Count() int {
	this.RLock()
	defer this.RUnlock()

	c := 0
	for i := 0; i < this.level; i++ {
		c += len(this.timerQueue[i])
	}
	return c
}

func (this *HashWheel) _add2WheelChain(tid uint32, t *SafeTimer, forceNext bool) error {
	defer func() error {
		if err := recover(); err != nil {
			logger.Error(fmt.Sprintf("add safetimer to hashwheel err: %s.", err))
			return errors.New(fmt.Sprintf("add safetimer to hashwheel err: %s.", err))
		} else {
			return nil
		}
	}()

	now := UnixTS()
	if t.unixts-now >= this.levelInterval || this.nextHashWheel == nil {

		for i := this.level - 1; i >= 0; i-- {
			if t.unixts-now >= int64(i)*this.levelInterval {
				if (i+this.index)%this.level == this.index && forceNext {
					this.timerQueue[(i+this.index+1)%this.level][tid] = t
				} else {
					this.timerQueue[(i+this.index)%this.level][tid] = t
				}
				break
			}
		}
		return nil
	} else {
		//应该放到下级
		return this.nextHashWheel.Add2WheelChain(tid, t)

	}
}

func (this *HashWheel) Add2WheelChain(tid uint32, t *SafeTimer) error {
	this.Lock()
	defer this.Unlock()

	t.SetTid(tid)
	//debug.PrintStack()
	return this._add2WheelChain(tid, t, false)
}

func (this *HashWheel) RemoveFromWheelChain(tid uint32) {
	this.Lock()
	defer this.Unlock()

	for i := 0; i < this.level; i++ {
		if _, ok := this.timerQueue[i][tid]; ok {
			delete(this.timerQueue[i], tid)
			return
		}
	}
	//去下级wheel找
	if this.nextHashWheel != nil {
		this.nextHashWheel.RemoveFromWheelChain(tid)
	}
}

func (this *HashWheel) GetTriggerWithIn(ms int64) map[uint32]*SafeTimer {
	leafWheel := this
	for leafWheel.nextHashWheel != nil {
		leafWheel = leafWheel.nextHashWheel
	}

	leafWheel.Lock()
	defer leafWheel.Unlock()

	triggerList := make(map[uint32]*SafeTimer)
	now := UnixTS()
	for k, v := range leafWheel.timerQueue[leafWheel.index] {
		if v.unixts-now <= ms {
			triggerList[k] = v
		}
	}

	for k, v := range triggerList {
		/*
			delete(leafWheel.timerQueue[leafWheel.index], k)
			if v.interval != 0 {
				//add next interval
				v.ResetUnixts()
				this.Add2WheelChain(v.GetTid(), v)
			}
		*/

		if v.interval == 0 {
			delete(leafWheel.timerQueue[leafWheel.index], k)
		} else {
			v.ResetUnixts()
		}
	}

	//add unness del
	return triggerList

}

//时间轮跑起来
func (this *HashWheel) RunWheel() {
	for {
		time.Sleep(time.Duration(this.levelInterval) * time.Millisecond)
		//loop
		this.Lock()
		triggerList := this.timerQueue[this.index]
		this.timerQueue[this.index] = make(map[uint32]*SafeTimer, this.maxCap)
		for k, v := range triggerList {
			this._add2WheelChain(k, v, true)
		}
		//下一格
		this.index = (this.index + 1) % this.level
		this.Unlock()
	}
}
