## 前言
一直以来都来研究`redis`的设计理念和实现思路，随着时间的推移有了想自己实现一版的想法，由于笔者本身是一名`java`开发，对于`C语言`中的某些理念不是非常熟悉，于是折中选用`go语言`进行实现，而这个开源项目将会记录笔者实现`mini-redis`的一些开发思路和实现的介绍。

`mini-redis`无论从函数名还是整体思路都基本沿用了原生`redis`的规范，唯一与C语言理念不同的是`go语言`本身对于`epoll`进行了封装，要想实现`redis`那种单线程处理所有指令的操作，笔者通过`channel`交互的方式来达到这一点。

同时，为了让读者更加直观的了解`redis`的实现核心脉络，笔者也在实现时也进行了一定简化，希望对那些想了解redis但是又不太熟悉`C语言`的开发朋友有所帮助。


## 如何部署运行mini-redis

这里笔者以`Linux`为例，我们找到项目中的`build-linux.sh`键入如下指令:

```bash
./build-linux.sh
```

执行该指令之后，我们的项目会生成一个bin的文件夹，其内部一个`redis-server`的可执行文件，此时我们通过Linux服务器执行`./redis-server`，最终项目会输出如下内容，说明项目成功运行了：

```bash
sharkchili@DESKTOPJ:~$ ./redis-server
2024/09/08 23:07:09 init redis server
2024/09/08 23:07:09 load redis server config
2024/09/08 23:07:09 this redis server address: localhost:6379
2024/09/08 23:07:09 event loop is listening and waiting for client connection.

```


此时我们就可以通过redis-cli连接mini-redis了：

```bash
sharkchili@DESKTOP-xxxx:~/redis/src$ ./redis-cli
127.0.0.1:6379> command
1) "COMMAND"
2) "PING"
127.0.0.1:6379> ping
PONG
127.0.0.1:6379>

```




## 目前的开发进度

截至目前，笔者完成了下面几个核心模块的开发：

- `mini-redis`服务端ip端口绑定初始化。
- `redis-cli`连接时将其封装为`redisClient`，并能够接收其命令请求。
- 解析`redis-cli`通过`RESP`协议发送的命令请求。
- 支持客户端键入command指令并回复当前服务端支持的指令集。
- 支持客户端键入PING指令感知mini-redis是否可用。
- 完成GET、SET指令指令解析和常规用法
- 完成列表LINDEX、LPOP、RPUSH、LRANGE指令,可作为内存消息队列


后续开发计划:

+ [x] 字典数据结构开发
+ [x] 列表底层数据结构双向链表
+ [x] 有序集合等数据结构开发
+ [x] 字符串常规`GET`指令开发调测
+ [x] 字符串`SET`指令开发调测
+ [x] 列表操作LINDEX、LPOP、RPUSH、LRANGE指令开发
+ [x] 字典操作HSET、HMSET、HSETNX、HGET、HMGET、HGETALL、HDEL指令开发
+ [x] 有序集合所有操作指令开发
+ [ ] `AOF`持久化和重载机制
+ [ ] `LRU`缓存置换算法
+ [ ] 性能压测


## 如何阅读源码

本项目目录结构为:
- `adlist.go` : redis底层双向链表实现 
- `adlist_test.go` : 双向链表测试单元 
- `client.go` : 处理redis-cli请求的客户端对象
- `command.go` : redis所有操作指令实现
- `db.go` : redis内存数据库
- `dict.go` : 哈希对象操作实现
- `networking.go` : 网络操作函数集
- `object.go` : redis对象创建函数
- `redis.conf` : 配置文件
- `redis.go` : redis服务端
- `t_hash.go` : 针对redis对象的哈希操作函数
- `t_list.go` : 基于adlist双向链表对于redis对象的链表操作函数
- `util.go` : mini-redis工具类
- `main.go` : mini-redis启动入口 
- `go.mod` 
- `build-windows.sh` : Windows下程序启动脚本 
- `build-linux.sh` : Linux启动脚本 
- `README.md` 





同时，在开发过程中的设计和实现也都会不断输出为文档，读者可以按需取用下面的文章来了解笔者的开发过程和实现思路：


来聊聊我用go手写redis这件事:<https://mp.weixin.qq.com/s?__biz=MzkwODYyNTM2MQ==&mid=2247486169&idx=1&sn=9b562eca113fbbe02d07f9ae2ccf0e79&chksm=c0c65e67f7b1d77166b7ff9a7a0403fb02d39ae1a111a73e12f7f1cdc3e79e91b2d116a5908a#rd>

mini-redis如何解析处理客户端请求
:<https://mp.weixin.qq.com/s?__biz=MzkwODYyNTM2MQ==&mid=2247486258&idx=1&sn=5a1bfffc075881e32cc247d6a76a88fe&chksm=c0c65f8cf7b1d69a91c1a9b3c5d15467e820db886e5e60ba059ccb3861b21d5e57ee8fc9b13d#rd>

实现mini-redis字符串操作:<https://mp.weixin.qq.com/s?__biz=MzkwODYyNTM2MQ==&mid=2247486268&idx=1&sn=003ba535c5a78c88cb4a15859edcfdf5&chksm=c0c65f82f7b1d69406832650746f93de3ef6b3b8a77badcf0f9924e1e052369990b4d233f10f#rd>


硬核复刻redis底层双向链表核心实现:<https://mp.weixin.qq.com/s?__biz=MzkwODYyNTM2MQ==&mid=2247486323&idx=1&sn=70812c54fa782e459d443951c0a39752&chksm=c0c65fcdf7b1d6dbfe783d1d8e11e270fc567921a5e1cdbb699f2bb7a1be5392d9a9c791ee25#rd>

聊聊我说如何用go语言实现redis列表操作:<https://mp.weixin.qq.com/s?__biz=MzkwODYyNTM2MQ==&mid=2247486416&idx=1&sn=1f5ad9ad17a80e2cd868ec8e33fb5015&chksm=c0c65f6ef7b1d6788012b590def4654e07153adcb18a7911fe87604f1eee55c1ba58e3c31012#rd>

动手复刻redis之go语言下的字典的设计与落地:<https://mp.weixin.qq.com/s?__biz=MzkwODYyNTM2MQ==&mid=2247486645&idx=1&sn=23a386c2fab95fbfd11b1b34ac1fbbcc&chksm=c0c6580bf7b1d11dde0655f93ae70265f16e8b693a0165d7b3144d9ac4d4a9d2c472b94b9d14#rd>

Go 语言下的 Redis 跳表设计与实现:<https://mp.weixin.qq.com/s/9ygqb7399drvkSl-V0zuSA>

Go 语言版 Redis 有序集合指令复刻探索:<https://mp.weixin.qq.com/s/u46O5HNopskSQtml2MHIow>

mini-redis复刻Redis的INCR指令:<https://mp.weixin.qq.com/s/BwACqJvE8krYA-dglmxrMw>

聊聊我基于dict优化mini-redis数据性能这件事:<https://mp.weixin.qq.com/s/H0M3ZD6o7bu4yiLiyyd9Kw>

基于AI IDE的mini-redis列表操作测试用例设计与功能验收:<https://mp.weixin.qq.com/s/fpWU_MgOq-1je5TijtBGHQ>

## 关于我

你好，我是 **SharkChili** ，禅与计算机程序设计艺术布道者，希望我的理念对您有所启发。

**📝 我的公众号：写代码的SharkChili**  
在这里，我会分享技术干货、编程思考与开源项目实践。

**🚀 我的开源项目：mini-redis**  
一个用于教学理解的 Redis 精简实现，欢迎 Star & Contribute：  
<https://github.com/shark-ctrl/mini-redis>

**👥 欢迎加入读者群**  
关注公众号，回复 **【加群】** 即可获取联系方式，期待与你交流技术、共同成长！

![image](https://github.com/user-attachments/assets/ed27dbdf-f0da-40b3-bb4e-cf948c4611a3)
