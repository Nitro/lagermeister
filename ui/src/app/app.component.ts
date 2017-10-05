import { Component, ViewEncapsulation, ChangeDetectorRef } from '@angular/core';
import { Http } from '@angular/http';
import * as _ from "lodash";

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss'],
  encapsulation: ViewEncapsulation.None
})

export class AppComponent {
  exampleSocket: any;
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
      // Fetch the chart config and begin setting up the chart objects
      this.http.get(`http://localhost:9010/config`)
      // this.http.get(`http://10.40.21.28:9010/config`)
        .subscribe( (resp: any) => {
          this.chartConfig = JSON.parse(resp.text());
          this.setupChartData();
        });
    }


    setupChartData() {
      let lineChartOptions: Object = {
        isStacked: true,
        legend: 'top',
        chartArea: {top: 30, left: 100, right: 120},
        height: 250,
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

      this.allCharts = {
        BatchSize: {
          chartType: 'LineChart',
          data: {},
          chart: null,
          domElement: 'batch-size-chart',
          columns: [['Timestamp', 'datetime'], ['Batch Size', 'number']],
          options: Object.assign({
            width: '100%',
            vAxis: {
              title: 'BatchSize',
            },
            colors: ['#4CAF50'],
          }, lineChartOptions)
        },
        Lag: {
          chartType: 'LineChart',
          data: {},
          chart: null,
          domElement: 'lag-chart',
          columns: [['Timestamp', 'datetime'], ['Lag (secs)', 'number']],
          options: Object.assign({
            width: 1000,
            vAxis: {
              title: 'Lag (secs)',
            },
            hAxis: {
              viewWindow: {}
            },
            colors: ['#E24500'],
          }, lineChartOptions)
        },
        LagGauge: {
          chartType: 'Gauge',
          data: {},
          chart: null,
          domElement: 'lag-gauge',
          columns: [['Label', 'string'], ['Lag (secs)', 'number']],
          options: {
            width: 200,
            height: 200,
            vAxis: {
              title: 'BatchSize',
            },
            colors: [],
            legend: 'top',
            chartArea: {top: 30, left: 0, right: 100},
          }
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
            colors: ['#FFC000', '#208DB9'],
          }, lineChartOptions)
        }
      };

      this.lastSeenValues = {};

      // Load the google charts lib, then begin drawing the charts
      let script = document.createElement('script');
      script.src = '//www.google.com/jsapi';
      script.onload = () => {
        (<any>window).google.load('visualization', '1', {'callback': this.drawChart.bind(this), 'packages':['corechart', 'gauge']});
      };
      document.head.appendChild(script);
    }

    /*
     * Function to setup the google charts and add them to the DOM
     */
    drawChart() {
      for(let i in this.allCharts) {
        // Add a google data table and chart to each chart object
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

        // Setup the columns for each chart
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

    /*
     * Setup a WebSocket connection to listen for any incoming messages
     * Create an interval to push new data to the charts every second
     */
    createWebsocketConnection() {
      this.exampleSocket = new WebSocket("ws://localhost:9010/messages");
      // this.exampleSocket = new WebSocket("ws://10.40.21.28:9010/messages");
      this.exampleSocket.onmessage = (event:any) => {
        this.aggregateData(JSON.parse(event.data));
      };

      // Push new data to the charts every second
      setInterval(() => {
        this.pendingData.forEach((data) => {
          this.updateChartData(data);
        });
        this.pendingThroughput.forEach((data) => {
          this.updateChartData(data);
        });
        this.pendingData.splice(0);
        this.pendingThroughput.splice(0);
      }, 1000)
    }

    /*
     * Function used to build the proper throughput metrics
     * And push the data to the pending array
     */
    aggregateData(data: any) {
      switch (data.MetricType) {
        case 'Throughput':

          this.pendingThroughput.push(data);
          let grouped = _.groupBy(this.pendingThroughput, (evt: any) => {
            return evt.Timestamp
          });

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

            combined.push(combinedItem);
          });
          this.pendingThroughput = combined;
          break;
        default:
          this.pendingData.push(data);
      }
    }

    /*
     * Function to call the relevant Update function based off metric type
     */
    updateChartData(data: any) {
      switch (data.MetricType) {
        case 'Lag':
          this.updateLineChart(this.allCharts[data.MetricType], data);
          this.updateGauge(this.allCharts['LagGauge'], data);
          this.updateLastSeenTable(data);
          break;

        default:
          this.updateLineChart(this.allCharts[data.MetricType], data);
          this.updateLastSeenTable(data);
      }
    }

    /*
     * Function to keep the last seen timestamps of each metric up to date
     */
    updateLastSeenTable(data: any) {
      let newValues = {};
      newValues[data.Sender + " (" + data.SourceIP + ")"] = new Date(data.Timestamp * 1000);
      Object.assign(this.lastSeenValues, newValues);
      // Lets angular know there are new changes to update the UI
      this.ref.detectChanges();
    }

    /*
     * Function used to move the line chart view window and redraw the chart with updated data
     */
    updateLineChart(chart: any, data: any) {
      let date = new Date(data.Timestamp * 1000);
      let min = new Date((data.Timestamp-60) * 1000);

      // Moves the viewWindow to give us the effect that the chart is scrolling
      chart.options.hAxis.viewWindow.min = min;
      chart.options.hAxis.viewWindow.max = date;

      if (!_.isNil(data.Values)) { // Draw multiple lines
        let fields = [date];
        for (let i in data.Values) {
          fields.push(data.Values[i])
        }
        chart.data.addRow(fields);
      } else { // Draw single line
        chart.data.addRow([date, data.Value]);
      }
      chart.chart.draw(chart.data, chart.options);
      this.removeHiddenRow(chart);
    }

    /*
     * Function to set the gauge values and redraw itself
     */
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
      this.removeHiddenRow(chart);
    }

    /*
     * Function used to stop the chart data getting too big
     * After adding a new row we remove an old one
     */
    removeHiddenRow(chart: any) {
      if (chart.data.getNumberOfRows() > 100) {
        chart.data.removeRow(0)
      }
    }
    /*
     * Used by the HTML ngFor to filter through the keys of lastSeenValues
     */
    get getLastSeenKeys() {
      return _.keys(this.lastSeenValues);
    }

}
