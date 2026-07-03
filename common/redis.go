package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var RDB *redis.Client
var RedisEnabled = true

func RedisKeyCacheSeconds() int {
	return SyncFrequency
}

// InitRedisClient This function is called after init()
func InitRedisClient() (err error) {
	if os.Getenv("REDIS_CONN_STRING") == "" {
		RedisEnabled = false
		SysLog("REDIS_CONN_STRING not set, Redis is not enabled")
		return nil
	}
	if os.Getenv("SYNC_FREQUENCY") == "" {
		SysLog("SYNC_FREQUENCY not set, use default value 60")
		SyncFrequency = 60
	}
	SysLog("Redis is enabled")
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	opt.PoolSize = GetEnvOrDefault("REDIS_POOL_SIZE", 10)
	RDB = redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = RDB.Ping(ctx).Result()
	if err != nil {
		FatalLog("Redis ping test failed: " + err.Error())
	}
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis connected to %s", opt.Addr))
		SysLog(fmt.Sprintf("Redis database: %d", opt.DB))
	}
	return err
}

func ParseRedisOption() *redis.Options {
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	return opt
}

func RedisSet(key string, value string, expiration time.Duration) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis SET: key=%s, value=%s, expiration=%v", key, value, expiration))
	}
	ctx := context.Background()
	return RDB.Set(ctx, key, value, expiration).Err()
}

func RedisGet(key string) (string, error) {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis GET: key=%s", key))
	}
	ctx := context.Background()
	val, err := RDB.Get(ctx, key).Result()
	return val, err
}

//func RedisExpire(key string, expiration time.Duration) error {
//	ctx := context.Background()
//	return RDB.Expire(ctx, key, expiration).Err()
//}
//
//func RedisGetEx(key string, expiration time.Duration) (string, error) {
//	ctx := context.Background()
//	return RDB.GetSet(ctx, key, expiration).Result()
//}

func RedisDel(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisDelKey(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL Key: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisHSetObj(key string, obj interface{}, expiration time.Duration) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSET: key=%s, obj=%+v, expiration=%v", key, obj, expiration))
	}
	ctx := context.Background()

	data := make(map[string]interface{})

	// 使用反射遍历结构体字段
	v := reflect.ValueOf(obj).Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Skip DeletedAt field
		if field.Type.String() == "gorm.DeletedAt" {
			continue
		}

		// 处理指针类型
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				data[field.Name] = ""
				continue
			}
			value = value.Elem()
		}

		// 处理布尔类型
		if value.Kind() == reflect.Bool {
			data[field.Name] = strconv.FormatBool(value.Bool())
			continue
		}

		// 其他类型直接转换为字符串
		data[field.Name] = fmt.Sprintf("%v", value.Interface())
	}

	txn := RDB.TxPipeline()
	txn.HSet(ctx, key, data)

	// 只有在 expiration 大于 0 时才设置过期时间
	if expiration > 0 {
		txn.Expire(ctx, key, expiration)
	}

	_, err := txn.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}
	return nil
}

func RedisHGetObj(key string, obj interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HGETALL: key=%s", key))
	}
	ctx := context.Background()

	result, err := RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to load hash from Redis: %w", err)
	}

	if len(result) == 0 {
		return fmt.Errorf("key %s not found in Redis", key)
	}

	// Handle both pointer and non-pointer values
	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("obj must be a pointer to a struct, got %T", obj)
	}

	v := val.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("obj must be a pointer to a struct, got pointer to %T", v.Interface())
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldName := field.Name
		if value, ok := result[fieldName]; ok {
			fieldValue := v.Field(i)

			// Handle pointer types
			if fieldValue.Kind() == reflect.Ptr {
				if value == "" {
					continue
				}
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				fieldValue = fieldValue.Elem()
			}

			// Enhanced type handling for Token struct
			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(value)
			case reflect.Int, reflect.Int64:
				intValue, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse int field %s: %w", fieldName, err)
				}
				fieldValue.SetInt(intValue)
			case reflect.Bool:
				boolValue, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("failed to parse bool field %s: %w", fieldName, err)
				}
				fieldValue.SetBool(boolValue)
			case reflect.Struct:
				// Special handling for gorm.DeletedAt
				if fieldValue.Type().String() == "gorm.DeletedAt" {
					if value != "" {
						timeValue, err := time.Parse(time.RFC3339, value)
						if err != nil {
							return fmt.Errorf("failed to parse DeletedAt field %s: %w", fieldName, err)
						}
						fieldValue.Set(reflect.ValueOf(gorm.DeletedAt{Time: timeValue, Valid: true}))
					}
				}
			default:
				return fmt.Errorf("unsupported field type: %s for field %s", fieldValue.Kind(), fieldName)
			}
		}
	}

	return nil
}

// incrIfTTLScript / hincrIfTTLScript: 在单条 Lua 脚本内原子地完成
// "TTL 检查 + 自增"。只有 key 存在且带 TTL 时才自增（避免把持久化
// 一个本应过期的缓存 key）；INCRBY/HINCRBY 本身不会改变已有 TTL。
// 返回 1 表示已自增，0 表示因 key 不存在/无 TTL 被跳过。
var (
	incrIfTTLScript = redis.NewScript(`
if redis.call('TTL', KEYS[1]) > 0 then
	redis.call('INCRBY', KEYS[1], ARGV[1])
	return 1
end
return 0`)
	hincrIfTTLScript = redis.NewScript(`
if redis.call('TTL', KEYS[1]) > 0 then
	redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[2])
	return 1
end
return 0`)
)

// RedisIncrEx 原子自增（仅当 key 存在且有 TTL）。
// 返回 applied=false 表示 key 不存在或无 TTL，自增被跳过。
func RedisIncrEx(key string, delta int64) (applied bool, err error) {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis INCR: key=%s, delta=%d", key, delta))
	}
	res, err := incrIfTTLScript.Run(context.Background(), RDB, []string{key}, delta).Int()
	if err != nil {
		return false, fmt.Errorf("redis incr failed: %w", err)
	}
	return res == 1, nil
}

// RedisIncr Add this function to handle atomic increments.
// 兼容旧语义：key 不存在/无 TTL 时静默跳过并返回 nil。
// 需要区分"被跳过"的调用方请使用 RedisIncrEx。
func RedisIncr(key string, delta int64) error {
	_, err := RedisIncrEx(key, delta)
	return err
}

// RedisHIncrByEx 原子哈希字段自增（仅当 key 存在且有 TTL）。
// 返回 applied=false 表示 key 不存在或无 TTL，自增被跳过。
func RedisHIncrByEx(key, field string, delta int64) (applied bool, err error) {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HINCRBY: key=%s, field=%s, delta=%d", key, field, delta))
	}
	res, err := hincrIfTTLScript.Run(context.Background(), RDB, []string{key}, field, delta).Int()
	if err != nil {
		return false, fmt.Errorf("redis hincrby failed: %w", err)
	}
	return res == 1, nil
}

// RedisHIncrBy 兼容旧语义：key 不存在/无 TTL 时静默跳过并返回 nil。
func RedisHIncrBy(key, field string, delta int64) error {
	_, err := RedisHIncrByEx(key, field, delta)
	return err
}

// RedisHSetNXField 仅当 hash 字段不存在时写入（HSETNX），已存在则不覆盖。
// 用于"回源初始化"场景：避免无条件 HSET 覆盖掉并发 HINCRBY 已落地的增量。
func RedisHSetNXField(key, field string, value interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSETNX field: key=%s, field=%s, value=%v", key, field, value))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if ttl > 0 {
		ctx := context.Background()
		txn := RDB.TxPipeline()

		if err := txn.HSetNX(ctx, key, field, value).Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}
	return nil
}

func RedisHSetField(key, field string, value interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSET field: key=%s, field=%s, value=%v", key, field, value))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if ttl > 0 {
		ctx := context.Background()
		txn := RDB.TxPipeline()

		hsetCmd := txn.HSet(ctx, key, field, value)
		if err := hsetCmd.Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}
	return nil
}
