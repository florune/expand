# 快捷词命名规范

推荐格式：

```text
:<domain>-<action>[-<target>][-<environment>]
```

示例：

- `:mysql-connect-prod`
- `:redis-cluster-info`
- `:kafka-offset-orders`
- `:docker-logs-admin`
- `:t2-restart-api-prod`

规则：

- 必须以 `:` 开头。
- 使用小写 ASCII、数字、连字符或下划线，不允许空格。
- 新建实例推荐使用连字符；旧 YAML 模板中的下划线继续兼容。
- 环境统一使用 `local`、`dev`、`test`、`stg`、`prod`。
- 生产相关快捷词必须显式包含 `prod`。
- 名称应包含动作，例如 `:mysql-connect-prod`，不要只写 `:mysql-prod`。
- 查询与修改操作分开命名，例如 `:kafka-offset-plan` 和 `:kafka-offset-apply`。
- 同一个加密用户空间中触发词必须唯一；不同本地用户可以拥有相同触发词和不同内容。
