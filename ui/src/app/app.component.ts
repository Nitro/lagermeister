import { Component, ViewEncapsulation, ChangeDetectorRef } from '@angular/core';
import { Http, Response } from '@angular/http';
import * as _ from "lodash";

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrls: ['../../node_modules/nvd3/build/nv.d3.css'],
  encapsulation: ViewEncapsulation.None
})

export class AppComponent {
  title = 'Working!';
  exampleSocket: any;
  chart: any;
  data: any;
  options: any;
  pendingData: Array<any> = [];
  pendingThroughput: Array<any> = [];
  lastSeenValues: Object;
  chartConfig: any;

  allCharts: {
    BatchSize: {
      data: Object;
      options: Object;
      chart: Object;
      domElement: string;
      columns: string[][];
      chartType: string;
    },
    Lag: {
      data: Object;
      options: Object;
      chart: Object;
      domElement: string;
      columns: string[][];
      chartType: string;
    },
    LagGauge: {
      data: Object;
      options: Object;
      chart: Object;
      domElement: string;
      columns: string[][];
      chartType: string;
    },
    Throughput: {
      data: Object;
      options: Object;
      chart: Object;
      domElement: string;
      columns: string[][];
      chartType: string;
    }
  };

    constructor(private ref: ChangeDetectorRef,
                private http: Http) {

      this.http.get(`http://localhost:9010/config`)
        .subscribe( (resp: any) => {
          this.chartConfig = JSON.parse(resp.text());
          console.log('chart config', this.chartConfig);
          this.setupChartData();
        });
    }


    setupChartData() {
      let lineChartOptions: Object = {
        isStacked: true,
        legend: 'top',
        chartArea: {top: 30, left: 100},
        width: '100%',
        height: 500,
        curveType: 'function',
        hAxis: {
          gridLines: {color: '#333'},
          viewWindow: {}
        },
        vAxis: {
          gridLines: {color: '#333'},
          viewWindow: {
            min: 0
          }
        }
      };

      let gaugeChartOptions: Object = {
        legend: 'top',
        chartArea: {top: 30, left: 100},
        width: 300,
        height: 300,
      };

      this.allCharts = {
        BatchSize: {
          chartType: 'LineChart',
          data: {},
          chart: null,
          domElement: 'batch-size-chart',
          columns: [['Timestamp', 'datetime'], ['Batch Size', 'number']],
          options: Object.assign({
            vAxis: {
              title: 'BatchSize',
            },
            colors: this.getRandomColor(),
          }, lineChartOptions)
        },
        Lag: {
          chartType: 'LineChart',
          data: {},
          chart: null,
          domElement: 'lag-chart',
          columns: [['Timestamp', 'datetime'], ['Lag (secs)', 'number']],
          options: Object.assign({
            vAxis: {
              title: 'Lag (secs)',
            },
            hAxis: {
              viewWindow: {}
            },
            colors: this.getRandomColor(),
          }, lineChartOptions)
        },
        LagGauge: {
          chartType: 'Gauge',
          data: {},
          chart: null,
          domElement: 'lag-gauge',
          columns: [['Label', 'string'], ['Lag (secs)', 'number']],
          options: Object.assign({
            vAxis: {
              title: 'BatchSize',
            },
            colors: this.getRandomColor(),
          }, gaugeChartOptions)
        },
        Throughput: {
          chartType: 'LineChart',
          data: {},
          chart: null,
          domElement: 'throughput-chart',
          columns: [['Timestamp', 'datetime']].concat(_.map(this.chartConfig.Throughput, (k: string, v: string) => { return [v, 'number']; })),
          options: Object.assign({
            vAxis: {
              title: 'Throughput (rps)',
            },
            hAxis: {
              viewWindow: {}
            },
            colors: this.getRandomColor(),
          }, lineChartOptions)
        }
      };

      // console.log('chart data', this.allCharts);

      this.lastSeenValues = {};

      let script = document.createElement('script');
      script.src = '//www.google.com/jsapi';
      script.onload = () => {
        (<any>window).google.load('visualization', '1', {'callback': this.drawChart.bind(this), 'packages':['corechart', 'gauge']});
      };

      document.head.appendChild(script); //or something of the like
    }

    drawChart() {
      for(let i in this.allCharts) {

        this.allCharts[i].data = new (<any>window).google.visualization.DataTable();
        let chart;
        switch (this.allCharts[i].chartType) {
          case 'LineChart':
            chart = new (<any>window).google.visualization.AreaChart(document.getElementsByClassName(this.allCharts[i].domElement)[0]);
            break;
          case 'Gauge':
            chart = new (<any>window).google.visualization.Gauge(document.getElementsByClassName(this.allCharts[i].domElement)[0]);
            break;
        }

        this.allCharts[i].columns.forEach((col:Array<string>) => {
          this.allCharts[i].data.addColumn(col[1], col[0]);
        });

        switch (this.allCharts[i].chartType) {
          case 'Gauge':
            this.allCharts[i].data.addRow(['', 0]);
            break;
        }

        chart.draw(this.allCharts[i].data, this.allCharts[i].options);
        this.allCharts[i].chart = chart;
      }

      this.createWebsocketConnection();
    }


    updateChartData(data: any) {
      this.updateLastSeenTable(data);
      switch (data.MetricType) {
        case 'Lag':
          this.updateLineChart(this.allCharts[data.MetricType], data);
          this.updateGauge(this.allCharts['LagGauge'], data);
          break;
        default:
          this.updateLineChart(this.allCharts[data.MetricType], data);
      }
    }

    updateLastSeenTable(data: any) {
      let newValues = {};
      newValues[data.Sender] = new Date(data.Timestamp*1000);
      Object.assign(this.lastSeenValues, newValues);
      this.ref.detectChanges();
    }

    get getLastSeenKeys() {
      return _.keys(this.lastSeenValues);
    }

    updateLineChart(chart: any, data: any) {
      let date = new Date(data.Timestamp * 1000);
      let min = new Date((data.Timestamp-60) * 1000);
      chart.options.hAxis.viewWindow.min = min;
      chart.options.hAxis.viewWindow.max = date;

      if (!_.isNil(data.Values)) {
        let fields = [date];
        for (let i in data.Values) {
          fields.push(data.Values[i])
        }
        chart.data.addRow(fields);
      } else {
        chart.data.addRow([date, data.Value]);
      }
      chart.chart.draw(chart.data, chart.options);
    }

    updateGauge(chart: any, data: any) {
      let extraOptions = {};
      if (data.Threshold) {
        extraOptions = {
          redFrom: data.Threshold.Error,
          redTo: data.Value > 200 ? data.Value : 200,
          yellowFrom: data.Threshold.Warn,
          yellowTo: data.Threshold.Error,
          max: data.Value > 200 ? data.Value : 200
        }
      }

      chart.data.setValue(0, 0, data.MetricType);
      chart.data.setValue(0, 1, data.Value);
      chart.chart.draw(chart.data,  Object.assign(chart.options, extraOptions));
    }

    createWebsocketConnection() {
      this.exampleSocket = new WebSocket("ws://localhost:9010/messages");
      this.exampleSocket.onmessage = (event:any) => {
          this.aggregateData(JSON.parse(event.data));
      };

      setInterval(() => {
        // this.pendingData.forEach((data) => {
        //   this.updateChartData(data);
        // });
        this.pendingThroughput.forEach((data) => {
          // console.log(data);
          this.updateChartData(data);
        });
        // this.pendingData.splice(0);
        this.pendingThroughput.splice(0);
      }, 1000)
    }

    aggregateData(data: any) {

      switch (data.MetricType) {
        case 'Throughput':
          this.pendingThroughput.push(data);
          let grouped = _.groupBy(this.pendingThroughput, (evt: any) => {
            return evt.Timestamp
          });
           // console.log('grouped:', grouped);

          let combined: Array<any> = [];
          _.forEach(grouped, (items: any, time: any) => {
              let combinedItem = {
                Values: {}
              };
              _.forEach(items, (item: any) => {
                Object.assign(combinedItem, item);
                combinedItem.Values[item.Sender] = item.Value;
              });

              // Make sure we have all the columns defined or the chart goes ape
              _.forEach(this.chartConfig.Throughput, (val: any, key: any) => {
                combinedItem.Values[key] = combinedItem.Values[key] ? combinedItem.Values[key] : 0;
              });
              // console.log(combinedItem);

              combined.push(combinedItem);
          });

          this.pendingThroughput = combined;
          break;
        default:
          this.pendingData.push(data);
      }
    }

    getRandomColor() {
      return _.shuffle(['D6532A', '208DB9', '4CAF50', '009688', '01A04B', 'FFC000']);
    }

}
