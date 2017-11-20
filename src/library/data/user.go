package data

import (
    "library"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
    log "github.com/sirupsen/logrus"
)
var user_data_path string
func init() {
    log.Println("user init.....")
    data_path := library.GetCurrentPath() + "/data"
    wpath := &library.WPath{data_path}
    wpath.Mkdir()

    user_data_path = data_path+"/wing.db"
    wfile := &library.WFile{user_data_path}

    if !wfile.Exists() {
        create := "CREATE TABLE `userinfo` (`id` INTEGER PRIMARY KEY AUTOINCREMENT,`username` VARCHAR(64) NULL,`password` VARCHAR(64) NULL,`created` TIMESTAMP default (datetime('now', 'localtime')));"
        db, err := sql.Open("sqlite3", data_path + "/wing.db")
        defer db.Close()
        if err != nil {
            log.Println("sqlite3 open error", err)
            return
        }
        _, err = db.Exec(create)
        if err != nil {
            log.Println("sqlite3 exec error", err)
            return
        }
        u := User{"admin", "admin"}
        u.Add()
    }


}

type User struct{
    Name string
    Password string
}

// 添加用户
func (user *User) Add() bool {
    db, err := sql.Open("sqlite3", user_data_path)
    if err != nil {
        log.Println("2-open sqlite3 error", err)
        return false
    }
    defer db.Close()

    //插入数据
    stmt, err := db.Prepare("INSERT INTO userinfo(username, password) values(?,?)")
    if err != nil {
        log.Println("2-open sqlite3 error", err)
        return false
    }
    defer stmt.Close()

    res, err := stmt.Exec(user.Name, user.Password)
    if err != nil {
        log.Println("2-open sqlite3 error", err)
        return false
    }

    id, err := res.LastInsertId()
    if err != nil {
        log.Println("2-open sqlite3 error", err)
        return false
    }

    log.Println(id)
    return true
}

//查询用户
func (user *User) Get() bool {

    db, err := sql.Open("sqlite3", user_data_path)
    if err != nil {
        log.Println("2-open sqlite3 error", err)
        return false
    }
    defer db.Close()

    sql_str := "SELECT id, username, password, strftime('%Y-%m-%d %H:%M:%S',created) as created FROM userinfo WHERE username = ? AND password = ?"
    stmt, err:= db.Prepare(sql_str)
    if err != nil {
        log.Println(err)
        return false
    }

    defer stmt.Close()

    res := stmt.QueryRow(user.Name, user.Password)
    var (
        id int64
        username string
        password string
        daytime string
    )

    err = res.Scan(&id, &username, &password, &daytime)
    if err != nil {
        return false
    }

    log.Println(id, username, password, daytime)
    return true
}

// 更新用户
func (user *User) Update() bool {
    if !user.Get() {
        return false
    }

    return true
}

// 删除用户
func (user *User) Delete() bool {

    return false
}