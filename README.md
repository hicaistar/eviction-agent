# Eviction agent

## Build
$ docker build -t eviction-agent:latest .

## Install
1. 配置 policy file 路径，并拷贝配置文件，修改。
   - $ cp ./install/config.json $POLICY_PATH
   - 根据 config.json 格式修改按需修改配置
   - 修改 ./install/evtAgent.yaml 文件的 POLICY_CONFIG_FILE 配置
2. 部署应用
   - 修改 evtAgent.yaml 配置日志路径等
   - kubectl create -f evtAgent.yaml
