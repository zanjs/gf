// Copyright 2017 gf Author(https://gitee.com/johng/gf). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://gitee.com/johng/gf.
// Web Server进程间通信

package ghttp

import (
    "os"
    "gitee.com/johng/gf/g/os/gproc"
    "gitee.com/johng/gf/g/encoding/gbinary"
    "strings"
    "gitee.com/johng/gf/g/util/gconv"
    "fmt"
    "gitee.com/johng/gf/g/encoding/gjson"
)

const (
    gMSG_START     = 10
    gMSG_RESTART   = 20
    gMSG_CORESTART = 30
    gMSG_SHUTDOWN  = 40
    gMSG_NEW_FORK  = 50
)

// 处理进程间消息
// 数据格式： 操作(8bit) | 参数(变长)
func handleProcessMsg() {
    for {
        if msg := gproc.Receive(); msg != nil {
            fmt.Println(gproc.Pid(), gproc.IsChild(), msg)
            act  := gbinary.DecodeToUint(msg.Data[0 : 1])
            data := msg.Data[1 : ]
            if gproc.IsChild() {
                // 子进程
                switch act {
                    // 开启所有Web Server(根据消息启动)
                    case gMSG_START:
                        if len(data) > 0 {
                            sfm := bufferToServerFdMap(data)
                            for k, v := range sfm {
                                GetServer(k).startServer(v)
                            }
                        } else {
                            serverMapping.RLockFunc(func(m map[string]interface{}) {
                                for _, v := range m {
                                    v.(*Server).startServer(nil)
                                }
                            })
                        }

                    // 子进程收到重启消息，那么将自身的ServerFdMap信息收集后发送给主进程，由主进程进行统一调度
                    case gMSG_RESTART:
                        // 创建新的服务进程，子进程自动从父进程复制文件描述来监听同样的端口
                        sfm := getServerFdMap()
                        p   := procManager.NewProcess(os.Args[0], os.Args, os.Environ())
                        for name, m := range sfm {
                            for fdk, fdv := range m {
                                if len(fdv) > 0 {
                                    s := ""
                                    for _, item := range strings.Split(fdv, ",") {
                                        array := strings.Split(item, "#")
                                        fd    := uintptr(gconv.Uint(array[1]))
                                        s     += fmt.Sprintf("%s#%d", array[0], len(p.GetAttr().Files))
                                        p.GetAttr().Files = append(p.GetAttr().Files, fd)
                                    }
                                    sfm[name][fdk] = strings.TrimRight(s, ",")
                                }
                            }
                        }
                        p.SetPpid(gproc.Ppid())
                        p.Run()
                        fmt.Println(procManager)
                        b, _ := gjson.Encode(sfm)
                        sendProcessMsg(p.Pid(),      gMSG_START,    b)
                        sendProcessMsg(gproc.Pid(),  gMSG_SHUTDOWN, nil)
                        sendProcessMsg(gproc.Ppid(), gMSG_NEW_FORK, gconv.Bytes(p.Pid()))

                    // 友好关闭服务链接并退出
                    case gMSG_SHUTDOWN:
                        serverMapping.RLockFunc(func(m map[string]interface{}) {
                            for _, v := range m {
                                v.(*Server).Shutdown()
                            }
                        })
                        return

                }
            } else {
                // 父进程
                switch act {
                    // 开启服务
                    case gMSG_START:
                        p := procManager.NewProcess(os.Args[0], os.Args, os.Environ())
                        p.Run()
                        sendProcessMsg(p.Pid(), gMSG_START, nil)

                    // 重启服务
                    case gMSG_RESTART:
                        // 向所有子进程发送重启命令，子进程将会搜集Web Server信息发送给父进程进行协调重启工作
                        procManager.Send(formatMsgBuffer(gMSG_RESTART, nil))

                    // 协调重启服务
                    case gMSG_NEW_FORK:
                        //sendProcessMsg(p.Pid(), gMSG_START, data)
                        // 关闭旧的服务进程
                        //sendProcessMsg(msg.Pid, gMSG_SHUTDOWN, nil)

                    // 关闭服务
                    case gMSG_SHUTDOWN:
                        procManager.Send(formatMsgBuffer(gMSG_SHUTDOWN, nil))

                }
            }
        }
    }
}

// 向进程发送操作消息
func sendProcessMsg(pid int, act int, data []byte) {
    gproc.Send(pid, formatMsgBuffer(act, data))
}

// 生成一条满足Web Server进程通信协议的消息
func formatMsgBuffer(act int, data []byte) []byte {
    return append(gbinary.EncodeUint8(uint8(act)), data...)
}
