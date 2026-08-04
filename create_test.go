package tdengine_gorm

import (
	"os"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"gorm.io/gorm/utils/tests"
)

/*
CREATE STABLE IF NOT EXISTS sensors (

	ts TIMESTAMP,       -- 时间戳
	temperature FLOAT,  -- 温度
	humidity FLOAT      -- 湿度

) TAGS (

	location BINARY(20),  -- 传感器的位置
	type BINARY(10)       -- 传感器的类型

);
*/
type Sensors struct {
	Tbname      string    `gorm:"column:tbname"`                   // 子表名
	Location    string    `gorm:"column:location;type:BINARY(20)"` // 传感器位置
	Type        string    `gorm:"column:type;type:BINARY(10)"`     // 传感器类型
	Ts          time.Time `gorm:"column:ts"`                       // 时间戳
	Temperature float64   `gorm:"column:temperature"`              // 温度
	Humidity    float64   `gorm:"column:humidity"`                 // 湿度
}

func openIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TDENGINE_GORM_TEST_DSN")
	if dsn == "" {
		t.Skip("set TDENGINE_GORM_TEST_DSN to run TDengine integration tests")
	}
	db, err := gorm.Open(Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			// 禁止使用复数格式创建table name
			SingularTable: true,
		},
	})
	if err != nil {
		t.Fatalf("open TDengine: %v", err)
	}
	return db
}

func TestCreate(t *testing.T) {
	db := openIntegrationDB(t)
	var meter = Sensors{
		Tbname:      "sensor1",
		Location:    "beijing",
		Type:        "temp",
		Ts:          time.Now(),
		Temperature: 25.6,
		Humidity:    68.3,
	}
	if err := db.Create(&meter).Error; err != nil {
		t.Fatalf("failed to create user, got error %v", err)
	}

	var result Sensors
	if err := db.Select("tbname,*").Where("location = ?", meter.Location).Find(&result).Error; err != nil {
		t.Fatalf("failed to query user, got error %v", err)
	}

	tests.AssertEqual(t, result, meter)

	type partialUser struct {
		Type string
	}
	var partialResult partialUser
	if err := db.Raw("select * from sensors where location = ?", meter.Location).Scan(&partialResult).Error; err != nil {
		t.Fatalf("failed to query partial, got error %v", err)
	}
}

func TestBatchCreate(t *testing.T) {
	db := openIntegrationDB(t)
	var sensorses = []Sensors{
		{Tbname: "sensor1", Location: "beijing", Type: "temp", Ts: time.Now(), Temperature: 25.6, Humidity: 68.3},
		{Tbname: "sensor2", Location: "shanghai", Type: "temp", Ts: time.Now(), Temperature: 26.1, Humidity: 65.7},
		{Tbname: "sensor3", Location: "guangzhou", Type: "temp", Ts: time.Now(), Temperature: 28.4, Humidity: 75.2},
		{Tbname: "sensor4", Location: "shenzhen", Type: "temp", Ts: time.Now(), Temperature: 27.8, Humidity: 72.1},
	}

	if err := db.CreateInBatches(&sensorses, 2).Error; err != nil {
		t.Fatalf("failed to create meters, got error %v", err)
	}

	var results []Sensors
	db.Find(&results)

	for _, m := range sensorses {
		var result Sensors
		if err := db.Where("location = ?", m.Location).Find(&result).Error; err != nil {
			t.Fatalf("failed to query meter, got error %v", err)
		}

		tests.AssertEqual(t, result, m)
	}
}

func TestCreateWithMap(t *testing.T) {
	db := openIntegrationDB(t)
	var sensors = Sensors{
		Tbname:      "sensor5",
		Location:    "chengdu",
		Type:        "temp",
		Ts:          time.Now(),
		Temperature: 24.3,
		Humidity:    70.5,
	}

	if err := db.Table("sensors").Create(&map[string]interface{}{
		"tbname":      sensors.Tbname,
		"location":    sensors.Location,
		"type":        sensors.Type,
		"ts":          sensors.Ts,
		"temperature": sensors.Temperature,
		"humidity":    sensors.Humidity,
	}).Error; err != nil {
		t.Fatalf("failed to create meter, got error %v", err)
	}

	var result Sensors
	if err := db.Find(&result, sensors.Location).Error; err != nil {
		t.Fatalf("failed to query meter, got error %v", err)
	}
	tests.AssertEqual(t, result, sensors)
}
