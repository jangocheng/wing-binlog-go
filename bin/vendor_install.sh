#!/usr/bin/env bash
current_path=$(cd `dirname $0`; pwd)
root_path=$(dirname $current_path)
vendor_path=$root_path"/vendor"

##添加当前目录和当前目录下的vendor目录到GOPATH环境变量
export GOPATH="$vendor_path:$root_path"
if [ ! -d "$vendor_path" ]; then
 mkdir "$vendor_path"
 mkdir "$vendor_path/src"
fi
echo "installing... go-sql-driver/mysql"
go get github.com/go-sql-driver/mysql
echo "installing... siddontang/go-mysql/canal"
go get github.com/siddontang/go-mysql/canal
echo "installing... siddontang/go-mysql/replication"
go get github.com/siddontang/go-mysql/replication
echo "installing... siddontang/go-mysql/mysql"
go get github.com/siddontang/go-mysql/mysql
echo "installing... BurntSushi/toml"
go get github.com/BurntSushi/toml
echo "installing... go-martini/martini"
go get github.com/go-martini/martini
echo "installing... gorilla/websocket"
go get github.com/gorilla/websocket
echo "installing... github.com/axgle/mahonia"
go get github.com/axgle/mahonia
echo "installing... github.com/hashicorp/consul"
go get github.com/hashicorp/consul
echo "installing... github.com/sirupsen/logrus"
go get github.com/sirupsen/logrus

find $vendor_path -name '*.git*' | xargs rm -rf
##cp -rf $vendor_path/src/* $vendor_path
##cp -rf $root_path/src/library $root_path/vendor/

echo "install complete"