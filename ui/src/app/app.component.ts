import { Component, OnInit, ViewEncapsulation } from '@angular/core';
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
  lastSeenValues: Object;

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
      thresholds: Object;
      chartType: string;
    },
    Throughput: {
      data: Object;
      options: Object;
      chart: Object;
      domElement: string;
      columns: string[][];
      chartType: string;
    },
    LastSeen: {
      data: Object;
      options: Object;
      chart: Object;
      domElement: string;
      columns: string[][];
      chartType: string;
    },
  };

    constructor() {
      let lineChartOptions: Object = {
        isStacked: true,
        legend: 'top',
        chartArea: {top: 30, left: 100},
        width: 2000,
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

      let tableOptions: Object = {
        legend: 'top',
        chartArea: {top: 30, left: 100},
        width: 300,
        height: 300,
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
          thresholds: {},
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
          columns: [['Timestamp', 'datetime'], ['HTTP Rcvr', 'number'], ['TCP Rcvr', 'number']],
          options: Object.assign({
            vAxis: {
              title: 'Throughput (rps)',
            },
            hAxis: {
              viewWindow: {}
            },
            colors: this.getRandomColor(),
          }, lineChartOptions)
        },
        LastSeen: {
          chartType: 'Table',
          data: {},
          chart: null,
          domElement: 'last-seen-table',
          columns: [['Sender'], ['Timestamp']],
          options: Object.assign({
            vAxis: {
              title: 'Last Message Received',
            },
          }, tableOptions)
        }
      };

      this.lastSeenValues = {};

      let script = document.createElement('script');
      script.src = '//www.google.com/jsapi';
      script.onload = () => {
        (<any>window).google.load('visualization', '1', {'callback': this.drawChart.bind(this), 'packages':['corechart', 'gauge', 'table']});
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
          case 'Table':
            chart = new (<any>window).google.visualization.Table(document.getElementsByClassName(this.allCharts[i].domElement)[0]);
            break;
        }

        this.allCharts[i].columns.forEach( (col: Array<string>) => {
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
      // this.updateLastSeenTable(data);
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

      console.log(data);
      // this.lastSeenValues[data.Sender] = data.Timestamp;
      //
      // let columns = this.allCharts.LastSeen.columns;
      // let fields = [columns[0][0], columns[1][0]];
      // console.log('Fields', fields);
      // for(let i in this.lastSeenValues) {
      //   fields.push(i, this.lastSeenValues[i])
      // }
      //
      // let chart = this.allCharts.LastSeen;
      // chart.data = new (<any>window).google.visualization.arrayToDataTable(fields, false);
      // this.lastSeenValues = {};
      // console.log(chart);
      // chart.chart.draw(chart.data, chart.options);
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
        let data = this.aggregateData(JSON.parse(event.data));
        this.pendingData.push(data);
      };

      setInterval(() => {
        this.pendingData.forEach((data) => {
          this.updateChartData(data);
        });
        this.pendingData.splice(0);
      }, 1000)
    }

    aggregateData(data: any) {
      switch (data.MetricType) {
        case 'Throughput':
          let throughputEvts = _.filter(this.pendingData, (evt: any) => {
            return evt.MetricType === 'Throughput' && (evt.Timestamp >= data.Timestamp - 1000)
          });

          data.Values = {};
          data.Values[data.Sender] = data.Value;

          data = _.reduce(throughputEvts, (memo: any, evt: any) => {
            memo.Values[evt.Sender] = evt.Value;
            return memo
          }, data);

          this.pendingData = _.filter(this. pendingData, (evt: any) => {
            return evt.MetricType !== 'Throughput' && (evt.Timestamp >= data.Timestamp - 1000)
          });

          return data;
        default:
          return data;
      }
    }

    getRandomColor() {
      return _.shuffle(['D6532A', '208DB9', '4CAF50', '009688', '01A04B', 'FFC000']);
    }

}
